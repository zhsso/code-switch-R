package services

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestProviderCompatibilityModeValidation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mode      string
		wantError bool
	}{
		{name: "empty keeps legacy behavior"},
		{name: "deepseek codex", mode: CompatibilityModeDeepSeekCodex},
		{name: "unknown", mode: "deepseek-chat", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := Provider{CompatibilityMode: tc.mode}
			errors := provider.ValidateConfiguration()
			if tc.wantError && len(errors) == 0 {
				t.Fatal("expected compatibility mode validation error")
			}
			if !tc.wantError && len(errors) != 0 {
				t.Fatalf("unexpected validation errors: %v", errors)
			}
		})
	}
}

func TestDuplicateProviderCopiesCompatibilityMode(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	source := Provider{
		ID:                1,
		Name:              "DeepSeek",
		APIURL:            "https://api.deepseek.com",
		APIKey:            "sk-test",
		Enabled:           true,
		CompatibilityMode: CompatibilityModeDeepSeekCodex,
	}
	if err := ps.SaveProviders(CodexPlatform, []Provider{source}); err != nil {
		t.Fatalf("save provider: %v", err)
	}

	cloned, err := ps.DuplicateProvider(CodexPlatform, source.ID)
	if err != nil {
		t.Fatalf("duplicate provider: %v", err)
	}
	if cloned.CompatibilityMode != CompatibilityModeDeepSeekCodex {
		t.Fatalf("compatibility mode not copied: %q", cloned.CompatibilityMode)
	}

	providers, err := ps.LoadProviders(CodexPlatform)
	if err != nil {
		t.Fatalf("load providers: %v", err)
	}
	if len(providers) != 2 || providers[1].CompatibilityMode != CompatibilityModeDeepSeekCodex {
		t.Fatalf("compatibility mode not persisted on clone: %+v", providers)
	}
}

func TestNormalizeDeepSeekCodexRequest(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","prompt_caching":{"enabled":true},"service_tier":"priority","reasoning":{"effort":"high","summary":"auto"},"tools":[{"type":"function","name":"lookup"}],"input":"hello"}`)
	cleaned, removed := normalizeProviderRequestBody(body, CompatibilityModeDeepSeekCodex)

	wantRemoved := []string{"prompt_caching", "reasoning.summary", "service_tier"}
	if !reflect.DeepEqual(removed, wantRemoved) {
		t.Fatalf("removed fields = %v, want %v", removed, wantRemoved)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &payload); err != nil {
		t.Fatalf("cleaned request is invalid JSON: %v", err)
	}
	for _, field := range []string{"prompt_caching", "service_tier"} {
		if _, exists := payload[field]; exists {
			t.Errorf("%s should be removed", field)
		}
	}
	var reasoning map[string]json.RawMessage
	if err := json.Unmarshal(payload["reasoning"], &reasoning); err != nil {
		t.Fatalf("reasoning is invalid: %v", err)
	}
	if _, exists := reasoning["summary"]; exists {
		t.Error("reasoning.summary should be removed")
	}
	if string(reasoning["effort"]) != `"high"` {
		t.Fatalf("reasoning.effort changed: %s", reasoning["effort"])
	}
	if string(payload["tools"]) != `[{"type":"function","name":"lookup"}]` {
		t.Fatalf("tools changed: %s", payload["tools"])
	}
	if string(payload["input"]) != `"hello"` {
		t.Fatalf("input changed: %s", payload["input"])
	}
}

func TestNormalizeDeepSeekCodexRequestPassthrough(t *testing.T) {
	for _, body := range []string{
		`{"model":"deepseek-v4-flash","reasoning":{"effort":"high"}}`,
		`{"prompt_caching":true,"prompt_caching":false}`,
		`not-json`,
	} {
		cleaned, removed := normalizeProviderRequestBody([]byte(body), CompatibilityModeDeepSeekCodex)
		if len(removed) != 0 || string(cleaned) != body {
			t.Fatalf("request should pass through unchanged: %q -> %q, removed=%v", body, cleaned, removed)
		}
	}

	body := []byte(`{"prompt_caching":true}`)
	cleaned, removed := normalizeProviderRequestBody(body, "")
	if len(removed) != 0 || string(cleaned) != string(body) {
		t.Fatal("empty compatibility mode must preserve the request")
	}
}

func TestDeepSeekPresetRelayFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupCaptureDBEnv(t)

	type capturedRequest struct {
		path          string
		authorization string
		body          []byte
	}
	var captured capturedRequest
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.path = r.URL.Path
		captured.authorization = r.Header.Get("Authorization")
		captured.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer upstream.Close()

	ps := NewProviderService()
	provider := Provider{
		ID:                     1,
		Name:                   "DeepSeek V4 Flash",
		APIURL:                 upstream.URL,
		APIKey:                 "deepseek-secret",
		Enabled:                true,
		APIEndpoint:            "/responses",
		ConnectivityAuthType:   "bearer",
		CompatibilityMode:      CompatibilityModeDeepSeekCodex,
		SupportedModels:        map[string]bool{"deepseek-v4-flash": true},
		ModelMapping:           map[string]string{"gpt-5.6-sol": "deepseek-v4-flash"},
		RequestSanitizeEnabled: true,
		SanitizeConfig:         &SanitizeConfig{BlockedBodyFields: slp("legacy_field")},
	}
	if err := ps.SaveProviders(CodexPlatform, []Provider{provider}); err != nil {
		t.Fatalf("save provider: %v", err)
	}

	prs := newTestRelayService(ps)
	if err := prs.SetRequestCapture(true); err != nil {
		t.Fatalf("enable capture: %v", err)
	}
	router := gin.New()
	router.POST("/responses", prs.proxyHandler(CodexPlatform, "/responses"))

	requestBody := `{"model":"gpt-5.6-sol","prompt_caching":true,"service_tier":"priority","reasoning":{"effort":"high","summary":"auto"},"tools":[{"type":"function","name":"lookup"}],"legacy_field":true,"input":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(requestBody))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("relay status = %d: %s", recorder.Code, recorder.Body.String())
	}

	if captured.path != "/responses" {
		t.Errorf("upstream path = %q", captured.path)
	}
	if captured.authorization != "Bearer deepseek-secret" {
		t.Errorf("upstream authorization = %q", captured.authorization)
	}

	var outbound map[string]json.RawMessage
	if err := json.Unmarshal(captured.body, &outbound); err != nil {
		t.Fatalf("outbound request is invalid JSON: %v", err)
	}
	if string(outbound["model"]) != `"deepseek-v4-flash"` {
		t.Errorf("mapped model = %s", outbound["model"])
	}
	for _, field := range []string{"prompt_caching", "service_tier", "legacy_field"} {
		if _, exists := outbound[field]; exists {
			t.Errorf("outbound request still contains %s", field)
		}
	}
	var reasoning map[string]json.RawMessage
	if err := json.Unmarshal(outbound["reasoning"], &reasoning); err != nil {
		t.Fatalf("outbound reasoning is invalid: %v", err)
	}
	if _, exists := reasoning["summary"]; exists || string(reasoning["effort"]) != `"high"` {
		t.Errorf("outbound reasoning = %s", outbound["reasoning"])
	}
	if _, exists := outbound["tools"]; !exists {
		t.Error("outbound tools were removed")
	}

	var loggedModel, loggedBody string
	if err := db.QueryRow(`SELECT model, request_body FROM request_log ORDER BY id DESC LIMIT 1`).
		Scan(&loggedModel, &loggedBody); err != nil {
		t.Fatalf("load request log: %v", err)
	}
	if loggedModel != "deepseek-v4-flash" {
		t.Errorf("logged model = %q", loggedModel)
	}
	if loggedBody != string(captured.body) {
		t.Errorf("captured body differs from upstream request:\nlog: %s\nupstream: %s", loggedBody, captured.body)
	}
}
