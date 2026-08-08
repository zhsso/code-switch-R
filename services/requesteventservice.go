package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
	"github.com/google/uuid"
)

const (
	RequestEventError     = "request_error"
	RequestEventSwitch    = "provider_switch"
	RequestEventCompleted = "request_completed"
)

// RequestEvent is one sanitized item in an inbound request's relay timeline.
// Payloads and credentials are deliberately absent from this structure.
type RequestEvent struct {
	ID             int64   `json:"id"`
	RequestID      string  `json:"request_id"`
	Platform       string  `json:"platform"`
	Model          string  `json:"model"`
	ModelGroupID   int64   `json:"model_group_id"`
	ModelGroupName string  `json:"model_group_name"`
	EventType      string  `json:"event_type"`
	Provider       string  `json:"provider"`
	FromProvider   string  `json:"from_provider"`
	ToProvider     string  `json:"to_provider"`
	Attempt        int     `json:"attempt"`
	Retry          int     `json:"retry"`
	HTTPCode       int     `json:"http_code"`
	ErrorType      string  `json:"error_type"`
	ErrorCode      string  `json:"error_code"`
	Message        string  `json:"message"`
	DurationSec    float64 `json:"duration_sec"`
	Outcome        string  `json:"outcome"`
	CreatedAt      string  `json:"created_at"`
}

// RequestEventInput is the write-side form of RequestEvent.
type RequestEventInput struct {
	RequestID      string
	Platform       string
	Model          string
	ModelGroupID   int64
	ModelGroupName string
	EventType      string
	Provider       string
	FromProvider   string
	ToProvider     string
	Attempt        int
	Retry          int
	HTTPCode       int
	ErrorType      string
	ErrorCode      string
	Message        string
	DurationSec    float64
	Outcome        string
}

type RequestEventService struct{}

func NewRequestEventService() *RequestEventService {
	return &RequestEventService{}
}

const requestEventInsertSQL = `
	INSERT INTO request_event_log (
		request_id, platform, model, model_group_id, model_group_name, event_type, provider,
		from_provider, to_provider, attempt, retry, http_code,
		error_type, error_code, message, duration_sec, outcome
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

func (s *RequestEventService) Record(input RequestEventInput) error {
	if s == nil {
		return nil
	}
	if err := requireCodexPlatform(input.Platform); err != nil {
		return err
	}
	if strings.TrimSpace(input.RequestID) == "" {
		return fmt.Errorf("request event id must not be empty")
	}
	if strings.TrimSpace(input.EventType) == "" {
		return fmt.Errorf("request event type must not be empty")
	}

	args := []interface{}{
		input.RequestID,
		CodexPlatform,
		strings.TrimSpace(input.Model),
		input.ModelGroupID,
		strings.TrimSpace(input.ModelGroupName),
		strings.TrimSpace(input.EventType),
		strings.TrimSpace(input.Provider),
		strings.TrimSpace(input.FromProvider),
		strings.TrimSpace(input.ToProvider),
		input.Attempt,
		input.Retry,
		input.HTTPCode,
		strings.TrimSpace(input.ErrorType),
		strings.TrimSpace(input.ErrorCode),
		truncateRequestEventMessage(redactSensitiveText(input.Message)),
		input.DurationSec,
		strings.TrimSpace(input.Outcome),
	}

	if GlobalDBQueue != nil {
		return GlobalDBQueue.Exec(requestEventInsertSQL, args...)
	}
	db, err := xdb.DB("default")
	if err != nil {
		return err
	}
	_, err = db.Exec(requestEventInsertSQL, args...)
	return err
}

func truncateRequestEventMessage(message string) string {
	message = strings.TrimSpace(message)
	const maxMessageBytes = 1024
	if len(message) <= maxMessageBytes {
		return message
	}
	return message[:maxMessageBytes] + "..."
}

func requestEventCutoff(days int) string {
	if days < 1 {
		days = 30
	}
	return time.Now().UTC().AddDate(0, 0, -days).Format(timeLayout)
}

// relayRequestTrace keeps one inbound request's attempts correlated without
// putting request bodies or upstream response text into the event table.
type relayRequestTrace struct {
	service        *RequestEventService
	requestID      string
	platform       string
	model          string
	modelGroupID   int64
	modelGroupName string
	attempt        int
	current        string
	lastError      error
	lastProvider   string
	clientAborted  bool
	terminalFailed bool
	hasIncident    bool
	succeeded      bool
	completed      bool
}

func newRelayRequestTrace(service *RequestEventService, platform string) *relayRequestTrace {
	return &relayRequestTrace{
		service:   service,
		requestID: uuid.NewString(),
		platform:  platform,
	}
}

func (trace *relayRequestTrace) RequestID() string {
	if trace == nil {
		return ""
	}
	return trace.requestID
}

func (trace *relayRequestTrace) SetModel(model string) {
	if trace != nil {
		trace.model = strings.TrimSpace(model)
	}
}

func (trace *relayRequestTrace) SetModelGroup(group ModelGroup) {
	if trace == nil {
		return
	}
	trace.modelGroupID = group.ID
	trace.modelGroupName = group.Name
}

func (trace *relayRequestTrace) BeforeAttempt(provider string) int {
	if trace == nil {
		return 0
	}
	trace.attempt++
	provider = strings.TrimSpace(provider)
	if trace.current != "" && provider != "" && trace.current != provider {
		trace.hasIncident = true
		_ = trace.record(RequestEventInput{
			RequestID:    trace.requestID,
			Platform:     trace.platform,
			Model:        trace.model,
			EventType:    RequestEventSwitch,
			FromProvider: trace.current,
			ToProvider:   provider,
			Attempt:      trace.attempt,
			ErrorType:    "provider_switch",
			Message:      trace.lastErrorMessage(),
			Outcome:      "continued",
		})
	}
	trace.current = provider
	trace.lastProvider = provider
	return trace.attempt
}

func (trace *relayRequestTrace) RecordForwardError(provider string, err error, attempt, retry int, duration time.Duration) {
	if trace == nil {
		return
	}
	if errors.Is(err, errClientAbort) {
		trace.clientAborted = true
		return
	}
	trace.lastError = err
	trace.lastProvider = strings.TrimSpace(provider)
	trace.hasIncident = true
	outcome := "continued"
	if errors.Is(err, errUpstreamStreamAborted) {
		trace.terminalFailed = true
		outcome = "failed"
	}
	errorType, errorCode, message, httpCode := requestEventErrorDetails(err)
	_ = trace.record(RequestEventInput{
		RequestID:   trace.requestID,
		Platform:    trace.platform,
		Model:       trace.model,
		EventType:   RequestEventError,
		Provider:    strings.TrimSpace(provider),
		Attempt:     attempt,
		Retry:       retry,
		HTTPCode:    httpCode,
		ErrorType:   errorType,
		ErrorCode:   errorCode,
		Message:     message,
		DurationSec: duration.Seconds(),
		Outcome:     outcome,
	})
}

func (trace *relayRequestTrace) RecordSummary(errorType, message string) {
	trace.recordLocalSummary("", errorType, message, "failed")
}

func (trace *relayRequestTrace) RecordLocalSummary(provider, errorType, message string) {
	trace.recordLocalSummary(provider, errorType, message, "continued")
}

func (trace *relayRequestTrace) recordLocalSummary(provider, errorType, message, outcome string) {
	if trace == nil {
		return
	}
	provider = strings.TrimSpace(provider)
	message = strings.TrimSpace(message)
	trace.lastError = errors.New(message)
	trace.lastProvider = provider
	trace.hasIncident = true
	if outcome == "failed" {
		trace.terminalFailed = true
	}
	_ = trace.record(RequestEventInput{
		RequestID: trace.requestID,
		Platform:  trace.platform,
		Model:     trace.model,
		EventType: RequestEventError,
		Provider:  provider,
		Attempt:   trace.attempt,
		ErrorType: strings.TrimSpace(errorType),
		ErrorCode: strings.TrimSpace(errorType),
		Message:   message,
		Outcome:   outcome,
	})
}

func (trace *relayRequestTrace) MarkFailed(err error) {
	if trace != nil {
		trace.lastError = err
		trace.terminalFailed = true
		trace.hasIncident = true
	}
}

func (trace *relayRequestTrace) MarkSucceeded() {
	if trace != nil {
		trace.succeeded = true
	}
}

func (trace *relayRequestTrace) Finish(status int, clientAborted bool) {
	if trace == nil || trace.completed {
		return
	}
	trace.completed = true
	clientAborted = clientAborted || trace.clientAborted
	if clientAborted && !trace.succeeded {
		return
	}
	outcome := "failed"
	if trace.succeeded {
		outcome = "success"
	} else if !trace.terminalFailed && status >= 200 && status < 300 {
		outcome = "success"
	}
	if !trace.hasIncident {
		return
	}
	errorType, errorCode, message, httpCode := requestEventErrorDetails(trace.lastError)
	if outcome == "success" {
		errorType, errorCode, message, httpCode = "", "", "", status
	}
	_ = trace.record(RequestEventInput{
		RequestID: trace.requestID,
		Platform:  trace.platform,
		Model:     trace.model,
		EventType: RequestEventCompleted,
		Provider:  trace.lastProvider,
		Attempt:   trace.attempt,
		HTTPCode:  httpCode,
		ErrorType: errorType,
		ErrorCode: errorCode,
		Message:   message,
		Outcome:   outcome,
	})
}

func (trace *relayRequestTrace) record(input RequestEventInput) error {
	if trace == nil || trace.service == nil {
		return nil
	}
	if input.ModelGroupID == 0 {
		input.ModelGroupID = trace.modelGroupID
	}
	if input.ModelGroupName == "" {
		input.ModelGroupName = trace.modelGroupName
	}
	return trace.service.Record(input)
}

func (trace *relayRequestTrace) lastErrorMessage() string {
	if trace == nil || trace.lastError == nil {
		return "provider selection changed"
	}
	return safeRelayError(trace.lastError)
}

func requestEventErrorDetails(err error) (errorType, errorCode, message string, httpCode int) {
	if err == nil {
		return "", "", "", 0
	}
	message = safeRelayError(err)
	switch {
	case errors.Is(err, errClientAbort):
		return "client_aborted", "client_aborted", message, 499
	case errors.Is(err, errUpstreamModelCapacity):
		return "model_capacity", "model_at_capacity", message, upstreamErrorHTTPCode(err)
	case errors.Is(err, errUpstreamStreamAborted):
		return "stream_aborted", "upstream_stream_aborted", message, upstreamErrorHTTPCode(err)
	case errors.Is(err, errUpstreamClientError):
		return "upstream_client_error", "upstream_rejected_request", message, upstreamErrorHTTPCode(err)
	case errors.Is(err, errEndpointPoolExhausted):
		return "endpoint_pool_exhausted", "endpoint_pool_exhausted", message, upstreamErrorHTTPCode(err)
	default:
		if isTimeoutError(err) {
			return "timeout", "upstream_timeout", message, 504
		}
		return "provider_error", "provider_request_failed", message, upstreamErrorHTTPCode(err)
	}
}

func upstreamErrorHTTPCode(err error) int {
	var statusErr *upstreamStatusError
	if errors.As(err, &statusErr) {
		return statusErr.status
	}
	return 0
}
