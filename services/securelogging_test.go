package services

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestSecureLogRedaction(t *testing.T) {
	const secret = "sk-secret-provider-token"
	for _, attr := range []slog.Attr{
		slog.String("curl", "curl -H 'Authorization: Bearer "+secret+"' --data secret-body"),
		slog.String("response", "upstream echoed "+secret),
		slog.String("error", "authorization: Bearer "+secret),
		slog.String("url", "https://user:"+secret+"@example.test/responses?api_key="+secret+"#fragment"),
	} {
		redacted := redactLogAttribute(nil, attr)
		if strings.Contains(redacted.Value.String(), secret) {
			t.Fatalf("attribute %q still contains credential: %q", attr.Key, redacted.Value.String())
		}
	}
}

func TestSafeRelayErrorDoesNotExposeUpstreamBody(t *testing.T) {
	const body = `{"error":"secret upstream body"}`
	err := &upstreamStatusError{status: 503, detail: "upstream status 503: " + body}
	if got := safeRelayError(err); got != "upstream HTTP 503" {
		t.Fatalf("safeRelayError = %q", got)
	}
	if got := safeRelayError(errors.New("request to https://host/?api_key=secret failed")); strings.Contains(got, "secret") {
		t.Fatalf("generic relay error leaked details: %q", got)
	}
}
