package services

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
)

const capacityMessage = "Selected model is at capacity. Please try a different model."

func TestModelCapacityErrorEnvelopeClassification(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "top-level error",
			body: `{"error":{"type":"server_error","message":"` + capacityMessage + `"}}`,
			want: true,
		},
		{
			name: "failed response",
			body: `{"type":"response.failed","response":{"status":"failed","error":{"message":"` + capacityMessage + `"}}}`,
			want: true,
		},
		{
			name: "plain upstream error",
			body: capacityMessage,
			want: true,
		},
		{
			name: "top-level overloaded code",
			body: `{"type":"error","code":"server_overloaded"}`,
			want: true,
		},
		{
			name: "failed response overloaded code",
			body: `{"type":"response.failed","response":{"status":"failed","error":{"code":"server_overloaded"}}}`,
			want: true,
		},
		{
			name: "normal generated output",
			body: `{"type":"response.completed","response":{"status":"completed","error":null,"output_text":"` + capacityMessage + `"}}`,
			want: false,
		},
		{
			name: "normal generated overloaded code text",
			body: `{"type":"response.completed","response":{"status":"completed","error":null,"output_text":"server_overloaded"}}`,
			want: false,
		},
		{
			name: "unrelated capacity error",
			body: `{"error":{"message":"request queue is at capacity"}}`,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isModelCapacityErrorEnvelope([]byte(tc.body)); got != tc.want {
				t.Fatalf("classification = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRelayResponseDetectsModelCapacityBeforeSSECommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/responses", nil)

	body := "event: response.created\n" +
		`data: {"type":"response.created","response":{"status":"in_progress"}}` + "\n\n" +
		"event: response.in_progress\n" +
		`data: {"type":"response.in_progress","response":{"status":"in_progress"}}` + "\n\n" +
		"event: response.failed\n" +
		`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_overloaded"}}}` + "\n\n"
	resp := xrequest.NewResponse(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	})
	relay := NewProviderRelayService(nil, nil, nil, nil, "127.0.0.1:0")
	ok, err := relay.relayResponseToClient(c, CodexPlatform, Provider{Name: "full"}, resp, true, &RequestLog{})

	if ok || !errors.Is(err, errUpstreamModelCapacity) {
		t.Fatalf("result = (%v, %v), want model-capacity failure", ok, err)
	}
	if errors.Is(err, errUpstreamStreamAborted) {
		t.Fatalf("lifecycle-only SSE events must remain switchable: %v", err)
	}
	if c.Writer.Written() || recorder.Body.Len() != 0 {
		t.Fatalf("capacity event leaked before fallback: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestRelayResponseForwardsLateModelCapacityWithoutMixingStreams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/responses", nil)

	body := "event: response.created\n" +
		`data: {"type":"response.created","response":{"status":"in_progress"}}` + "\n\n" +
		"event: response.output_text.delta\n" +
		`data: {"type":"response.output_text.delta","delta":"partial output"}` + "\n\n" +
		"event: response.failed\n" +
		`data: {"type":"response.failed","response":{"status":"failed","error":{"message":"` + capacityMessage + `"}}}` + "\n\n"
	resp := xrequest.NewResponse(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	})
	relay := NewProviderRelayService(nil, nil, nil, nil, "127.0.0.1:0")
	ok, err := relay.relayResponseToClient(c, CodexPlatform, Provider{Name: "late"}, resp, true, &RequestLog{})

	if ok || !errors.Is(err, errUpstreamModelCapacity) || !errors.Is(err, errUpstreamStreamAborted) {
		t.Fatalf("result = (%v, %v), want committed capacity failure", ok, err)
	}
	if !strings.Contains(recorder.Body.String(), "partial output") ||
		!strings.Contains(recorder.Body.String(), capacityMessage) {
		t.Fatalf("late capacity event must be forwarded intact: %q", recorder.Body.String())
	}
}

func TestRelayResponseDoesNotTreatGeneratedCapacityTextAsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/responses", nil)

	body := "event: response.created\n" +
		`data: {"type":"response.created","response":{"status":"in_progress"}}` + "\n\n" +
		"event: response.in_progress\n" +
		`data: {"type":"response.in_progress","response":{"status":"in_progress"}}` + "\n\n" +
		"event: response.output_text.delta\n" +
		`data: {"type":"response.output_text.delta","delta":"` + capacityMessage + `"}` + "\n\n"
	resp := xrequest.NewResponse(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	})
	relay := NewProviderRelayService(nil, nil, nil, nil, "127.0.0.1:0")
	ok, err := relay.relayResponseToClient(c, CodexPlatform, Provider{Name: "normal"}, resp, true, &RequestLog{})

	if !ok || err != nil {
		t.Fatalf("normal generated text result = (%v, %v)", ok, err)
	}
	if recorder.Body.String() != body {
		t.Fatalf("normal generated text changed: %q", recorder.Body.String())
	}
}

func TestForwardRequestFallsBackFromCapacitySSEToSecondAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRenameTestEnv(t)

	var primaryHits, fallbackHits atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Upstream", "primary")
		_, _ = io.WriteString(w, "event: response.created\n")
		_, _ = io.WriteString(w,
			`data: {"type":"response.created","response":{"status":"in_progress"}}`+"\n\n")
		_, _ = io.WriteString(w, "event: response.in_progress\n")
		_, _ = io.WriteString(w,
			`data: {"type":"response.in_progress","response":{"status":"in_progress"}}`+"\n\n")
		_, _ = io.WriteString(w, "event: response.failed\n")
		_, _ = io.WriteString(w,
			`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_overloaded"}}}`+"\n\n")
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fallbackHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream", "fallback")
		_, _ = io.WriteString(w, `{"id":"response-from-fallback"}`)
	}))
	defer fallback.Close()

	relay := newTestRelayService(NewProviderService())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{"model":"gpt-test","stream":true}`))
	provider := Provider{
		ID: 1, Name: "multi-address", APIURL: primary.URL, APIKey: "key", Enabled: true,
		FallbackAPIURLs: []string{fallback.URL},
	}
	ok, err := relay.forwardRequest(c, CodexPlatform, provider, "/responses",
		map[string]string{}, map[string]string{},
		[]byte(`{"model":"gpt-test","stream":true}`), true, "gpt-test", 0)

	if !ok || err != nil {
		t.Fatalf("fallback result = (%v, %v)", ok, err)
	}
	if primaryHits.Load() != 1 || fallbackHits.Load() != 1 {
		t.Fatalf("upstream hits: primary=%d fallback=%d", primaryHits.Load(), fallbackHits.Load())
	}
	if recorder.Header().Get("X-Upstream") != "fallback" {
		t.Fatalf("rejected response headers leaked: %v", recorder.Header())
	}
	if strings.Contains(recorder.Body.String(), "server_overloaded") ||
		!strings.Contains(recorder.Body.String(), "response-from-fallback") {
		t.Fatalf("unexpected client response: %q", recorder.Body.String())
	}
}

func TestProxyHandlerSwitchesProviderOnHTTP400ModelCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupBlacklistFixEnv(t)
	setAppSetting(t, "enable_blacklist", "true")
	setAppSetting(t, "blacklist_level_enabled", "false")
	setAppSetting(t, "blacklist_failure_threshold", "3")
	config := DefaultBlacklistLevelConfig()
	config.RetryWaitSeconds = 0
	if err := NewSettingsService().SaveBlacklistLevelConfig(config); err != nil {
		t.Fatal(err)
	}

	var capacityHits, healthyHits atomic.Int32
	capacityUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		capacityHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"server_overloaded"}}`)
	}))
	defer capacityUpstream.Close()
	healthyUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		healthyHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"response-from-next-provider"}`)
	}))
	defer healthyUpstream.Close()

	providerService := NewProviderService()
	if err := providerService.SaveProviders(CodexPlatform, []Provider{
		{ID: 1, Name: "capacity", APIURL: capacityUpstream.URL, APIKey: "key-1", Enabled: true, Level: 1},
		{ID: 2, Name: "healthy", APIURL: healthyUpstream.URL, APIKey: "key-2", Enabled: true, Level: 1},
	}); err != nil {
		t.Fatal(err)
	}
	relay := newTestRelayService(providerService)
	router := gin.New()
	router.POST("/responses", relay.proxyHandler(CodexPlatform, "/responses"))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/responses",
		strings.NewReader(`{"model":"gpt-test","stream":false}`))
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "response-from-next-provider") {
		t.Fatalf("relay response = %d %q", recorder.Code, recorder.Body.String())
	}
	if capacityHits.Load() != 1 || healthyHits.Load() != 1 {
		t.Fatalf("capacity error must switch immediately: capacity=%d healthy=%d",
			capacityHits.Load(), healthyHits.Load())
	}
}

func TestProxyHandlerSwitchesProviderOnSSEPreludeOverload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupBlacklistFixEnv(t)
	setAppSetting(t, "enable_blacklist", "true")
	setAppSetting(t, "blacklist_level_enabled", "false")
	setAppSetting(t, "blacklist_failure_threshold", "3")
	config := DefaultBlacklistLevelConfig()
	config.RetryWaitSeconds = 0
	if err := NewSettingsService().SaveBlacklistLevelConfig(config); err != nil {
		t.Fatal(err)
	}

	var overloadedHits, healthyHits atomic.Int32
	overloadedUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		overloadedHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Upstream", "overloaded")
		_, _ = io.WriteString(w, "event: response.created\n")
		_, _ = io.WriteString(w,
			`data: {"type":"response.created","response":{"status":"in_progress"}}`+"\n\n")
		_, _ = io.WriteString(w, "event: response.in_progress\n")
		_, _ = io.WriteString(w,
			`data: {"type":"response.in_progress","response":{"status":"in_progress"}}`+"\n\n")
		_, _ = io.WriteString(w, "event: response.failed\n")
		_, _ = io.WriteString(w,
			`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_overloaded"}}}`+"\n\n")
	}))
	defer overloadedUpstream.Close()
	healthyUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		healthyHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Upstream", "healthy")
		_, _ = io.WriteString(w, "event: response.created\n")
		_, _ = io.WriteString(w,
			`data: {"type":"response.created","response":{"status":"in_progress"}}`+"\n\n")
		_, _ = io.WriteString(w, "event: response.output_text.delta\n")
		_, _ = io.WriteString(w,
			`data: {"type":"response.output_text.delta","delta":"response-from-next-provider"}`+"\n\n")
	}))
	defer healthyUpstream.Close()

	providerService := NewProviderService()
	if err := providerService.SaveProviders(CodexPlatform, []Provider{
		{ID: 1, Name: "overloaded", APIURL: overloadedUpstream.URL, APIKey: "key-1", Enabled: true, Level: 1},
		{ID: 2, Name: "healthy", APIURL: healthyUpstream.URL, APIKey: "key-2", Enabled: true, Level: 1},
	}); err != nil {
		t.Fatal(err)
	}
	relay := newTestRelayService(providerService)
	router := gin.New()
	router.POST("/responses", relay.proxyHandler(CodexPlatform, "/responses"))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/responses",
		strings.NewReader(`{"model":"gpt-test","stream":true}`))
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "response-from-next-provider") {
		t.Fatalf("relay response = %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("X-Upstream") != "healthy" || strings.Contains(recorder.Body.String(), "server_overloaded") {
		t.Fatalf("overloaded response leaked: headers=%v body=%q", recorder.Header(), recorder.Body.String())
	}
	if overloadedHits.Load() != 1 || healthyHits.Load() != 1 {
		t.Fatalf("SSE overload must switch immediately: overloaded=%d healthy=%d",
			overloadedHits.Load(), healthyHits.Load())
	}
}
