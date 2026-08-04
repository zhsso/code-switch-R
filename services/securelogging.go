package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/daodao97/xgo/xlog"
)

var sensitiveLogPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization["':= ]+bearer[ ]+)[^ ,;"']+`),
	regexp.MustCompile(`(?i)(x-api-key["':= ]+)[^ ,;"']+`),
	regexp.MustCompile(`(?i)(api[_-]?key["':= ]+)[^ ,;"']+`),
}

func init() {
	ConfigureSecureLogging()
}

// ConfigureSecureLogging keeps dependency diagnostics useful without allowing
// SQL arguments, HTTP headers, request bodies, or credentials into stdout.
func ConfigureSecureLogging() {
	xlog.SetLogger(xlog.StdoutText(
		xlog.WithLevel(slog.LevelInfo),
		xlog.WithReplaceAttr(redactLogAttribute),
	))
}

func redactLogAttribute(_ []string, attr slog.Attr) slog.Attr {
	key := strings.ToLower(strings.TrimSpace(attr.Key))
	switch key {
	case "args", "body", "curl", "full_sql", "header", "headers", "request", "response":
		return slog.String(attr.Key, "[redacted]")
	case "url":
		return slog.String(attr.Key, sanitizeLogURL(fmt.Sprint(attr.Value.Any())))
	case "err", "error":
		return slog.String(attr.Key, redactSensitiveText(fmt.Sprint(attr.Value.Any())))
	default:
		if attr.Value.Kind() == slog.KindString {
			return slog.String(attr.Key, redactSensitiveText(attr.Value.String()))
		}
		return attr
	}
}

func redactSensitiveText(value string) string {
	for _, pattern := range sensitiveLogPatterns {
		value = pattern.ReplaceAllString(value, `${1}[redacted]`)
	}
	return value
}

func sanitizeLogURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return redactSensitiveText(strings.SplitN(strings.SplitN(raw, "?", 2)[0], "#", 2)[0])
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func safeTransportError(err error) string {
	if err == nil {
		return "unknown error"
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "request canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "request timed out"
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		if networkError.Timeout() {
			return "network timeout"
		}
		return "network transport error"
	}
	return "request failed"
}

func safeRelayError(err error) string {
	if err == nil {
		return "unknown error"
	}
	var statusError *upstreamStatusError
	if errors.As(err, &statusError) {
		return fmt.Sprintf("upstream HTTP %d", statusError.status)
	}
	switch {
	case errors.Is(err, errClientAbort):
		return "client disconnected"
	case errors.Is(err, errUpstreamStreamAborted):
		return "upstream stream interrupted"
	case errors.Is(err, errUpstreamClientError):
		return "upstream rejected request payload"
	case errors.Is(err, errEndpointPoolExhausted):
		return "provider endpoint pool exhausted"
	default:
		return safeTransportError(err)
	}
}
