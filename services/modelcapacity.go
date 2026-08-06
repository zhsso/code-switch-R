package services

import (
	"bytes"
	"errors"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	modelCapacityMessage      = "selected model is at capacity"
	modelOverloadedCode       = "server_overloaded"
	modelServerOverloadedCode = "server_is_overloaded"
	modelCapacityProbeSize    = 64 << 10
)

var errUpstreamModelCapacity = errors.New("upstream model is at capacity")

func containsModelCapacityMessage(data []byte) bool {
	text := strings.ToLower(string(data))
	return strings.Contains(text, modelCapacityMessage) ||
		(strings.Contains(text, "model") &&
			strings.Contains(text, "at capacity") &&
			strings.Contains(text, "different model"))
}

func containsModelCapacitySignal(data []byte) bool {
	return containsModelCapacityMessage(data) ||
		containsModelOverloadedCode(data)
}

func isModelOverloadedCode(value string) bool {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'`)) {
	case modelOverloadedCode, modelServerOverloadedCode:
		return true
	default:
		return false
	}
}

func containsModelOverloadedCode(data []byte) bool {
	text := strings.ToLower(string(data))
	return strings.Contains(text, modelOverloadedCode) ||
		strings.Contains(text, modelServerOverloadedCode)
}

func hasStructuredModelOverloadedCode(data []byte) bool {
	for _, path := range []string{
		"code",
		"error",
		"error.code",
		"response.error",
		"response.error.code",
		"codex_error_info",
	} {
		value := gjson.GetBytes(data, path)
		if value.Exists() && value.Type == gjson.String && isModelOverloadedCode(value.String()) {
			return true
		}
	}
	return false
}

func isModelCapacityErrorEnvelope(data []byte) bool {
	if !containsModelCapacitySignal(data) {
		return false
	}
	if !gjson.ValidBytes(data) {
		return strings.EqualFold(strings.TrimSpace(string(data)),
			"Selected model is at capacity. Please try a different model.") ||
			isModelOverloadedCode(string(data))
	}
	if hasStructuredModelOverloadedCode(data) {
		return true
	}

	typeName := strings.ToLower(gjson.GetBytes(data, "type").String())
	status := strings.ToLower(gjson.GetBytes(data, "status").String())
	responseStatus := strings.ToLower(gjson.GetBytes(data, "response.status").String())
	errorValue := gjson.GetBytes(data, "error")
	responseError := gjson.GetBytes(data, "response.error")
	return (errorValue.Exists() && errorValue.Type != gjson.Null) ||
		(responseError.Exists() && responseError.Type != gjson.Null) ||
		strings.Contains(typeName, "error") || strings.Contains(typeName, "failed") ||
		status == "failed" || responseStatus == "failed" ||
		containsModelCapacityMessage([]byte(gjson.GetBytes(data, "message").String()))
}

func parseSSEEvent(event []byte) (string, []byte) {
	normalized := strings.ReplaceAll(string(event), "\r\n", "\n")
	var eventName string
	dataLines := make([]string, 0, 1)
	for _, line := range strings.Split(normalized, "\n") {
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return strings.ToLower(eventName), []byte(strings.Join(dataLines, "\n"))
}

func isModelCapacitySSEEvent(event []byte) bool {
	eventName, data := parseSSEEvent(event)
	if !containsModelCapacitySignal(data) {
		return false
	}
	if eventName == "error" || strings.Contains(eventName, "failed") {
		return true
	}
	return isModelCapacityErrorEnvelope(data)
}

func isBufferedSSEPrelude(event []byte) bool {
	eventName, data := parseSSEEvent(event)
	if eventName == "" && len(bytes.TrimSpace(data)) == 0 {
		return true
	}
	if eventName == "" && gjson.ValidBytes(data) {
		eventName = strings.ToLower(gjson.GetBytes(data, "type").String())
	}
	switch eventName {
	case "response.queued",
		"response.created",
		"response.in_progress",
		// Responses emits these metadata-only events before the first text or
		// tool-argument delta. Keep them buffered so an immediate capacity
		// failure can still switch providers without committing a stream.
		"response.output_item.added",
		"response.content_part.added",
		"response.reasoning_summary_part.added",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.reasoning_summary_part.done",
		"response.completed",
		"response.done":
		return true
	default:
		return false
	}
}

func cutSSEEvent(data []byte) (event, rest []byte, ok bool) {
	lfIndex := bytes.Index(data, []byte("\n\n"))
	crlfIndex := bytes.Index(data, []byte("\r\n\r\n"))
	index, delimiterLength := -1, 0
	switch {
	case lfIndex >= 0 && (crlfIndex < 0 || lfIndex < crlfIndex):
		index, delimiterLength = lfIndex, 2
	case crlfIndex >= 0:
		index, delimiterLength = crlfIndex, 4
	default:
		return nil, data, false
	}
	return data[:index], data[index+delimiterLength:], true
}

// modelCapacityProbeWriter delays lifecycle-only SSE events until the first
// substantive event. This keeps the response switchable when an upstream sends
// response.created/in_progress before immediately failing with no output.
// Capacity errors after substantive output are forwarded but still reported as
// failures so the provider is not incorrectly marked healthy.
type modelCapacityProbeWriter struct {
	dst              http.ResponseWriter
	header           http.Header
	statusCode       int
	stream           bool
	committed        bool
	capacityDetected bool
	pending          bytes.Buffer
	ssePending       []byte
}

func newModelCapacityProbeWriter(dst http.ResponseWriter, stream bool) *modelCapacityProbeWriter {
	return &modelCapacityProbeWriter{
		dst:    dst,
		header: dst.Header().Clone(),
		stream: stream,
	}
}

func (w *modelCapacityProbeWriter) Header() http.Header {
	return w.header
}

func (w *modelCapacityProbeWriter) WriteHeader(statusCode int) {
	if w.committed || w.statusCode != 0 {
		return
	}
	w.statusCode = statusCode
}

func (w *modelCapacityProbeWriter) Write(data []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}

	if w.stream {
		if !w.committed {
			_, _ = w.pending.Write(data)
		}
		capacity, shouldCommit := w.inspectSSE(data)
		if capacity {
			w.capacityDetected = true
			if !w.committed {
				return 0, errUpstreamModelCapacity
			}
		}
		if w.committed {
			return w.dst.Write(data)
		}
		if shouldCommit || w.pending.Len() >= modelCapacityProbeSize {
			if err := w.commit(); err != nil {
				return 0, err
			}
		}
		return len(data), nil
	}

	if w.committed {
		return w.dst.Write(data)
	}
	_, _ = w.pending.Write(data)
	if isModelCapacityErrorEnvelope(w.pending.Bytes()) {
		w.capacityDetected = true
		return 0, errUpstreamModelCapacity
	}
	if gjson.ValidBytes(w.pending.Bytes()) || w.pending.Len() >= modelCapacityProbeSize {
		if err := w.commit(); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}

func (w *modelCapacityProbeWriter) Flush() {
	if !w.committed {
		return
	}
	if flusher, ok := w.dst.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *modelCapacityProbeWriter) Finish() error {
	if w.stream && len(w.ssePending) > 0 && isModelCapacitySSEEvent(w.ssePending) {
		w.capacityDetected = true
	}
	if w.capacityDetected {
		return errUpstreamModelCapacity
	}
	if !w.committed {
		return w.commit()
	}
	return nil
}

func (w *modelCapacityProbeWriter) inspectSSE(data []byte) (capacity, shouldCommit bool) {
	w.ssePending = append(w.ssePending, data...)
	for {
		event, rest, ok := cutSSEEvent(w.ssePending)
		if !ok {
			if len(w.ssePending) > modelCapacityProbeSize {
				w.ssePending = append([]byte(nil), w.ssePending[len(w.ssePending)-modelCapacityProbeSize:]...)
			}
			return capacity, shouldCommit
		}
		if isModelCapacitySSEEvent(event) {
			capacity = true
		}
		if !isBufferedSSEPrelude(event) {
			shouldCommit = true
		}
		w.ssePending = append(w.ssePending[:0], rest...)
	}
}

func (w *modelCapacityProbeWriter) commit() error {
	if w.committed {
		return nil
	}
	destinationHeader := w.dst.Header()
	for key, values := range w.header {
		destinationHeader[key] = append([]string(nil), values...)
	}
	statusCode := w.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	w.dst.WriteHeader(statusCode)
	w.committed = true
	if w.pending.Len() == 0 {
		return nil
	}
	_, err := w.dst.Write(w.pending.Bytes())
	w.pending.Reset()
	return err
}
