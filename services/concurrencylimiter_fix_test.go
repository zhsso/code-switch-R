package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// Provider-level maxConcurrency is a removed setting. Legacy values must not
// serialize or restrict the relay path.
func TestProviderMaxConcurrencyIsIgnored(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)

	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		entered <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer upstream.Close()

	ps := NewProviderService()
	if err := ps.SaveProviders(CodexPlatform, []Provider{
		{ID: 1, Name: "Unlimited", APIURL: upstream.URL, APIKey: "k", Enabled: true, MaxConcurrency: 1},
	}); err != nil {
		t.Fatalf("预置供应商失败: %v", err)
	}
	prs := newTestRelayService(ps)
	router := gin.New()
	router.POST("/responses", prs.proxyHandler(CodexPlatform, "/responses"))

	codes := make(chan int, 2)
	for range 2 {
		go func() {
			req := httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"m"}`))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			codes <- w.Code
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("两个请求应同时进入同一 Provider")
		}
	}
	close(release)
	for range 2 {
		if code := <-codes; code != http.StatusOK {
			t.Fatalf("并发请求应成功: %d", code)
		}
	}
	if hits.Load() != 2 {
		t.Fatalf("上游应收到两个并发请求: %d", hits.Load())
	}
}
