package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireCodexPlatform(t *testing.T) {
	if err := requireCodexPlatform("codex"); err != nil {
		t.Fatalf("codex should be accepted: %v", err)
	}
	for _, platform := range []string{"claude", "gemini", "custom:tool", ""} {
		if err := requireCodexPlatform(platform); err == nil {
			t.Errorf("%q should be rejected", platform)
		}
	}
}

func TestRelayRegistersCodexRoutesOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	relay := NewProviderRelayService(
		NewProviderService(),
		NewBlacklistService(nil, nil),
		nil,
		nil,
		"",
	)
	router := gin.New()
	relay.registerRoutes(router)

	for _, path := range []string{
		"/v1/messages",
		"/gemini/v1beta/models/example:generateContent",
		"/custom/example/v1/messages",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, recorder.Code)
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader("not-json"))
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("POST /responses = %d, want 400", recorder.Code)
	}
}
