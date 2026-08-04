package services

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestSpeedTest_WarmupReusesConnection 验证热身请求的响应 Body 被读完并关闭后，
// 连接能归还连接池供测量请求复用：同一端点的两次请求只应建立 1 条 TCP 连接。
// 若热身响应未关闭，测量请求会新开连接，延迟包含完整握手，热身失去意义。
func TestSpeedTest_WarmupReusesConnection(t *testing.T) {
	var newConns int32
	var headRequests int32
	var getRequests int32

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			atomic.AddInt32(&headRequests, 1)
			w.WriteHeader(http.StatusOK)
			return
		}
		atomic.AddInt32(&getRequests, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 128<<10))
	}))
	server.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt32(&newConns, 1)
		}
	}
	server.Start()
	defer server.Close()

	s := NewSpeedTestService()
	timeout := 5
	results := s.TestEndpoints([]string{server.URL}, &timeout)

	if len(results) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(results))
	}
	if results[0].Error != nil {
		t.Fatalf("测速不应失败: %s", *results[0].Error)
	}
	if results[0].Latency == nil {
		t.Fatal("期望返回延迟值")
	}

	if got := atomic.LoadInt32(&newConns); got != 1 {
		t.Errorf("热身+测量应复用同一连接（1 条），实际建立 %d 条", got)
	}
	if atomic.LoadInt32(&headRequests) != 1 || atomic.LoadInt32(&getRequests) != 1 {
		t.Fatalf("warmup must use HEAD before one measured GET: HEAD=%d GET=%d",
			atomic.LoadInt32(&headRequests), atomic.LoadInt32(&getRequests))
	}
}
