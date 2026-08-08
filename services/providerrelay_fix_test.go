package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"
)

func TestProviderRelayStopWaitsForTrackedRequest(t *testing.T) {
	relay := NewProviderRelayService(nil, nil, nil, nil, "127.0.0.1:0")
	entered := make(chan struct{})
	release := make(chan struct{})
	requestDone := make(chan struct{})

	handler := relay.trackRequest(func(c *gin.Context) {
		close(entered)
		<-release
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	go func() {
		defer close(requestDone)
		handler(ctx)
	}()
	<-entered

	stopDone := make(chan error, 1)
	go func() {
		stopDone <- relay.Stop()
	}()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before the active request completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(release)
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("tracked request did not complete")
	}
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("Stop returned an error after request drain: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after the active request completed")
	}
}

func TestProviderRelayRejectsTrackedRequestAfterStop(t *testing.T) {
	relay := NewProviderRelayService(nil, nil, nil, nil, "127.0.0.1:0")
	if err := relay.Stop(); err != nil {
		t.Fatal(err)
	}

	called := false
	handler := relay.trackRequest(func(c *gin.Context) {
		called = true
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	handler(ctx)

	if called {
		t.Fatal("relay invoked a handler after request admission was closed")
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

// TestSanitizeUpstreamHeadersDropsClientCredentials 客户端自带的认证头必须在转发前清掉,
// 否则用户本机的真实 API Key 会随请求发给链路上每一个第三方供应商。
// cloneHeaders 保留的是 Go 规范化后的键名(X-Api-Key),小写字面量 delete 删不掉。
func TestSanitizeUpstreamHeadersDropsClientCredentials(t *testing.T) {
	headers := map[string]string{
		"X-Api-Key":           "client-real-key",
		"x-GoOg-aPi-KeY":      "client-google-key",
		"Authorization":       "Bearer client-token",
		"Api-Key":             "another",
		"Accept-Encoding":     "gzip, deflate",
		"Connection":          "keep-alive",
		"Content-Type":        "application/json",
		"OpenAI-Organization": "org-test",
	}

	sanitizeUpstreamHeaders(headers)

	for _, name := range []string{"x-api-key", "x-goog-api-key", "authorization", "api-key", "accept-encoding", "connection"} {
		if got := getHeaderFold(headers, name); got != "" {
			t.Errorf("头 %s 应被清理,实际仍为 %q", name, got)
		}
	}
	// 非凭据业务头必须保留。
	if headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type 被误删")
	}
	if headers["OpenAI-Organization"] == "" {
		t.Errorf("OpenAI-Organization 被误删")
	}
}

// TestSetHeaderCanonicalReplacesOtherCasing 注入供应商凭据时必须覆盖客户端同名头的其它大小写形式。
// xrequest 是 req.Header[k] = []string{v} 直接赋值不做规范化,
// 若残留 X-Api-Key 与新写的 x-api-key,两个条目会同时发到上游。
func TestSetHeaderCanonicalReplacesOtherCasing(t *testing.T) {
	headers := map[string]string{"X-Api-Key": "client-key"}

	setHeaderCanonical(headers, "x-api-key", "provider-key")

	if len(headers) != 1 {
		t.Fatalf("期望只剩 1 个 x-api-key 条目,实际 %d 个: %v", len(headers), headers)
	}
	if headers["X-Api-Key"] != "provider-key" {
		t.Errorf("期望规范化键 X-Api-Key=provider-key,实际 %v", headers)
	}
}

// TestForwardRequestSendsOnlyProviderCredentials 端到端确认转发到上游的请求里
// 只有本代理注入的供应商凭据,且没有把客户端 Accept-Encoding 透传出去。
func TestForwardRequestSendsOnlyProviderCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmpHome := setupRenameTestEnv(t)
	_ = tmpHome

	type captured struct {
		apiKeyValues   []string
		authorization  []string
		acceptEncoding string
	}
	var got captured

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.apiKeyValues = r.Header.Values("X-Api-Key")
		got.authorization = r.Header.Values("Authorization")
		got.acceptEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	prs := newTestRelayService(NewProviderService())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req, err := http.NewRequest("POST", "/responses", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	// 客户端带上自己的凭据与压缩协商，模拟 Codex 的真实请求头。
	req.Header.Set("X-Api-Key", "client-real-key")
	req.Header.Set("Authorization", "Bearer client-token")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	c.Request = req

	provider := Provider{
		Name:                 "p1",
		APIURL:               upstream.URL,
		APIKey:               "provider-secret",
		Enabled:              true,
		ConnectivityAuthType: "x-api-key",
	}
	ok, ferr := prs.forwardRequest(c, CodexPlatform, provider, "/responses",
		map[string]string{}, cloneHeaders(req.Header), []byte(`{"model":"m"}`), false, "m")
	if !ok {
		t.Fatalf("转发应成功,实际失败: %v", ferr)
	}

	if len(got.apiKeyValues) != 1 {
		t.Errorf("上游应只收到 1 个 x-api-key,实际 %d 个: %v", len(got.apiKeyValues), got.apiKeyValues)
	}
	if len(got.apiKeyValues) > 0 && got.apiKeyValues[0] != "provider-secret" {
		t.Errorf("x-api-key 应为供应商密钥,实际 %q", got.apiKeyValues[0])
	}
	for _, v := range got.apiKeyValues {
		if v == "client-real-key" {
			t.Errorf("客户端真实密钥泄漏到上游")
		}
	}
	if len(got.authorization) != 0 {
		t.Errorf("x-api-key 认证模式下不应残留客户端 Authorization,实际 %v", got.authorization)
	}
	// Go 会自行加 Accept-Encoding: gzip 并自动解压;透传客户端的值会让响应体保持压缩,
	// SSE 与 usage 解析随之失效
	if strings.Contains(got.acceptEncoding, "deflate") {
		t.Errorf("客户端 Accept-Encoding 被透传到上游: %q", got.acceptEncoding)
	}
}

// TestIsClientSideUpstreamStatus 上游 4xx 的分类:请求内容问题不该计入供应商失败,
// 而密钥失效/路径配错/限流仍属供应商侧。
func TestIsClientSideUpstreamStatus(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusRequestEntityTooLarge, true},
		{http.StatusUnprocessableEntity, true},
		{http.StatusUnsupportedMediaType, true},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
		{http.StatusBadGateway, false},
		{http.StatusOK, false},
	}
	for _, tc := range cases {
		if got := isClientSideUpstreamStatus(tc.status); got != tc.want {
			t.Errorf("isClientSideUpstreamStatus(%d) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// TestMaskSensitiveQuery URL 查询参数中的凭据必须在日志中脱敏。
func TestMaskSensitiveQuery(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/responses?trace=1", "/responses?trace=1"},
		{"/responses?stream=true&key=secret", "/responses?stream=true&key=***"},
		{"/responses?KEY=secret", "/responses?KEY=***"},
		{"/responses?access_token=tok&stream=true", "/responses?access_token=***&stream=true"},
	}
	for _, tc := range cases {
		if got := maskSensitiveQuery(tc.in); got != tc.want {
			t.Errorf("maskSensitiveQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCodexUsageFromNonStreamingResponse 非流式 /responses 直接返回 Response 对象,
// usage 在根级而非 response.usage,漏解析会让 token 与成本全部记 0。
func TestCodexUsageFromNonStreamingResponse(t *testing.T) {
	body := `{"id":"resp_1","object":"response","service_tier":"flex","usage":{
		"input_tokens":1000,"output_tokens":300,
		"input_tokens_details":{"cached_tokens":400},
		"output_tokens_details":{"reasoning_tokens":120}}}`

	usage := &RequestLog{}
	CodexParseTokenUsageFromResponse(body, usage)

	if usage.InputTokens != 600 {
		t.Errorf("InputTokens = %d, want 600(1000-400 缓存读取)", usage.InputTokens)
	}
	if usage.CacheReadTokens != 400 {
		t.Errorf("CacheReadTokens = %d, want 400", usage.CacheReadTokens)
	}
	if usage.ReasoningTokens != 120 {
		t.Errorf("ReasoningTokens = %d, want 120", usage.ReasoningTokens)
	}
	// output_tokens 已含 reasoning_tokens,计费引擎是 OutputCost+ReasoningCost 相加,
	// 不拆开会把推理 token 计两次
	if usage.OutputTokens != 180 {
		t.Errorf("OutputTokens = %d, want 180(300-120 推理),否则推理 token 被重复计费", usage.OutputTokens)
	}
	if usage.ServiceTier == "" {
		t.Errorf("根级 service_tier 未被解析")
	}
}

// TestCodexUsageStreamingStillParsed 流式 response.completed 事件的口径不能被上面的修复破坏。
func TestCodexUsageStreamingStillParsed(t *testing.T) {
	body := `{"type":"response.completed","response":{"service_tier":"default","usage":{
		"input_tokens":50,"output_tokens":20,
		"output_tokens_details":{"reasoning_tokens":8}}}}`

	usage := &RequestLog{}
	CodexParseTokenUsageFromResponse(body, usage)

	if usage.InputTokens != 50 || usage.OutputTokens != 12 || usage.ReasoningTokens != 8 {
		t.Errorf("流式解析结果不符: input=%d output=%d reasoning=%d, want 50/12/8",
			usage.InputTokens, usage.OutputTokens, usage.ReasoningTokens)
	}
}

// TestEnsureRequestLogCreatedAtOnLegacyTable SQLite 不允许 ALTER TABLE ADD COLUMN 带
// CURRENT_TIMESTAMP 这类非常量默认值。建表时没有 created_at 的旧库若走通用迁移会直接失败,
// 让 InitDatabase 报错、应用无法启动;迁移后新插入的行还必须能拿到时间戳。
func TestEnsureRequestLogCreatedAtOnLegacyTable(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	// 旧库:没有 created_at 列,且已有数据(空表不会触发 SQLite 的非常量默认值限制)
	if _, err := db.Exec(`CREATE TABLE request_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT, platform TEXT, model TEXT, provider TEXT)`); err != nil {
		t.Fatalf("建旧表失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO request_log (platform) VALUES ('codex')`); err != nil {
		t.Fatalf("写历史数据失败: %v", err)
	}

	if err := ensureRequestLogCreatedAt(db); err != nil {
		t.Fatalf("created_at 迁移失败(旧库将无法启动应用): %v", err)
	}

	// 历史行必须被回填
	var historical sql.NullString
	if err := db.QueryRow(`SELECT created_at FROM request_log WHERE id = 1`).Scan(&historical); err != nil {
		t.Fatalf("查询历史行失败: %v", err)
	}
	if !historical.Valid || historical.String == "" {
		t.Errorf("历史行 created_at 未回填")
	}

	// 迁移出来的列没有默认值,新插入行要靠触发器补时间戳,
	// 否则按时间统计的用量与成本全部失效
	if _, err := db.Exec(`INSERT INTO request_log (platform) VALUES ('codex')`); err != nil {
		t.Fatalf("插入新行失败: %v", err)
	}
	var fresh sql.NullString
	if err := db.QueryRow(`SELECT created_at FROM request_log WHERE platform = 'codex'`).Scan(&fresh); err != nil {
		t.Fatalf("查询新行失败: %v", err)
	}
	if !fresh.Valid || fresh.String == "" {
		t.Errorf("迁移后新插入行的 created_at 为 NULL,时间维度统计会失效")
	}

	// 幂等:重复迁移不应报错
	if err := ensureRequestLogCreatedAt(db); err != nil {
		t.Errorf("重复迁移应幂等,实际报错: %v", err)
	}
}

// TestEnsureRequestLogTableOnFreshDB 新建库走完整建表 + 迁移路径应无错,
// 且 created_at 默认值仍然生效。
func TestEnsureRequestLogTableOnFreshDB(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	if err := ensureRequestLogTableWithDB(db); err != nil {
		t.Fatalf("新库建表失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO request_log (platform) VALUES ('codex')`); err != nil {
		t.Fatalf("插入失败: %v", err)
	}
	var createdAt sql.NullString
	if err := db.QueryRow(`SELECT created_at FROM request_log`).Scan(&createdAt); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if !createdAt.Valid || createdAt.String == "" {
		t.Errorf("新库 created_at 应由默认值填充")
	}

	// service_tier 等后续追加列也应就位
	for _, col := range []string{"ephemeral_5m_tokens", "ephemeral_1h_tokens", "service_tier"} {
		exists, err := requestLogColumnExists(db, col)
		if err != nil {
			t.Fatalf("查询列 %s 失败: %v", col, err)
		}
		if !exists {
			t.Errorf("列 %s 缺失", col)
		}
	}
}

// TestRelayHTTPClientReusesTransport 转发必须共用连接池。
// xrequest 的默认路径每次调用都新建 http.Client 与 http.Transport,
// 连接零复用、空闲连接与读写协程长期滞留。
func TestRelayHTTPClientReusesTransport(t *testing.T) {
	if relayHTTPClient == nil || relayHTTPClient.Transport == nil {
		t.Fatal("relayHTTPClient 未配置共享 Transport")
	}
	transport, ok := relayHTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport 类型异常: %T", relayHTTPClient.Transport)
	}
	if transport.MaxIdleConnsPerHost <= 1 {
		t.Errorf("MaxIdleConnsPerHost = %d,连接无法有效复用", transport.MaxIdleConnsPerHost)
	}
	if transport.IdleConnTimeout == 0 {
		t.Errorf("IdleConnTimeout 未设置,空闲连接不会被回收")
	}
	if relayHTTPClient.Timeout == 0 {
		t.Errorf("客户端未设置兜底超时")
	}
}

// TestProxyHandlerRejectsInvalidJSONBody 空 body / 非法 JSON 会被每个上游各拒一次,
// 还会白耗一轮降级,应在入口直接挡掉。
func TestProxyHandlerRejectsInvalidJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRenameTestEnv(t)

	prs := newTestRelayService(NewProviderService())
	router := gin.New()
	router.POST("/responses", prs.proxyHandler(CodexPlatform, "/responses"))

	for _, body := range []string{"", "not-json", "{\"model\":"} {
		recorder := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/responses", strings.NewReader(body))
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("body=%q 期望 400,实际 %d", body, recorder.Code)
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Errorf("body=%q 响应不是 JSON: %v", body, err)
		}
	}
}

// TestStripCredentialQueryParams 查询串中的真实凭据不能转发给降级链上的第三方供应商；
// 非凭据参数必须原样保留。
func TestStripCredentialQueryParams(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空查询串", "", ""},
		{"剔除 key", "stream=true&key=secret", "stream=true"},
		{"大小写不敏感", "KEY=secret&stream=true", "stream=true"},
		{"剔除多种凭据", "key=a&access_token=b&api_key=c&alt=sse&token=d", "alt=sse"},
		{"只有凭据时清空", "key=secret", ""},
		{"非凭据参数原样保留", "alt=sse&pageSize=10", "alt=sse&pageSize=10"},
		{"值中的等号与编码不被改写", "alt=sse&filter=a%3Db%3Dc", "alt=sse&filter=a%3Db%3Dc"},
		{"无值参数保留", "prettyPrint&alt=sse", "prettyPrint&alt=sse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripCredentialQueryParams(tc.in); got != tc.want {
				t.Errorf("stripCredentialQueryParams(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRespondAllProvidersFailedStatus 全部供应商都以"请求内容有问题"拒绝时必须回 4xx。
// 回 502 会让 SDK 按服务端故障自动重试,一个永远不可能成功的坏请求被反复重发,
// 每次都完整扫一遍全部供应商、白耗上游配额。
func TestRespondAllProvidersFailedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name            string
		lastError       error
		allClientErrors bool
		wantStatus      int
	}{
		{"供应商故障回 502", errors.New("upstream status 503"), true, http.StatusBadGateway},
		{"全部都是请求内容被拒回 400", fmt.Errorf("%w: upstream status 400", errUpstreamClientError), true, http.StatusBadRequest},
		// 混合失败：降级链末尾那个挑剔的备用供应商回 400，不能掩盖前面"临时过载、稍后可用"的供应商。
		// 回 400 会让 SDK 放弃重试，用户拿到"请求格式有问题"，而请求对前一个供应商完全合法
		{"混合失败维持 502", fmt.Errorf("%w: upstream status 400", errUpstreamClientError), false, http.StatusBadGateway},
		{"客户端中断按供应商故障口径", fmt.Errorf("%w: canceled", errClientAbort), true, http.StatusBadGateway},
		{"无错误信息回 502", nil, true, http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request, _ = http.NewRequest("POST", "/responses", nil)

			respondAllProvidersFailed(c, tc.lastError, tc.allClientErrors, gin.H{"error": "all failed"})

			if recorder.Code != tc.wantStatus {
				t.Errorf("状态码 = %d, want %d", recorder.Code, tc.wantStatus)
			}
		})
	}
}

// TestForwardRequestDegradesWhenNothingWritten 上游返回 2xx 但响应体在写出任何字节之前就读失败时,
// 仍应按普通失败上报以便降级到下一个供应商;判成"已部分写出"会白白放弃可用的供应商,
// 客户端只拿到一个空的 200。
func TestForwardRequestDegradesWhenNothingWritten(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRenameTestEnv(t)

	// 上游声明 SSE 且给出 Content-Length,但只写一半就断开,让读取阶段失败
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("da"))
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	}))
	defer upstream.Close()

	prs := newTestRelayService(NewProviderService())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request, _ = http.NewRequest("POST", "/responses", strings.NewReader(`{"model":"m"}`))

	provider := Provider{Name: "p1", APIURL: upstream.URL, APIKey: "k", Enabled: true}
	ok, err := prs.forwardRequest(c, CodexPlatform, provider, "/responses",
		map[string]string{}, map[string]string{}, []byte(`{"model":"m"}`), true, "m")

	if ok {
		t.Fatalf("上游中途断开不应判为成功")
	}
	// 关键：没有写出任何字节时不能标成 errUpstreamStreamAborted，否则调用方会放弃降级
	if c.Writer.Written() {
		t.Skip("本次上游在断开前已写出字节，不构成待测场景")
	}
	if errors.Is(err, errUpstreamStreamAborted) {
		t.Errorf("未写出任何字节却被判为已部分写出，调用方会放弃降级: %v", err)
	}
}

// TestCheckNonStreamTruncated 非流式响应被上游截断时必须能识别出来。
// xrequest 内部 `body, _ := io.ReadAll(...)` 丢弃了读错误，截断的响应会被当成完整响应，
// 半死的供应商在非流式请求上永远被记成功、永远不会被拉黑。
func TestCheckNonStreamTruncated(t *testing.T) {
	newResp := func(contentLength int64, encoding string) *xrequest.Response {
		header := http.Header{}
		if encoding != "" {
			header.Set("Content-Encoding", encoding)
		}
		return &xrequest.Response{RawResponse: &http.Response{
			ContentLength: contentLength,
			Header:        header,
		}}
	}

	cases := []struct {
		name      string
		resp      *xrequest.Response
		written   int64
		wantError bool
	}{
		{"完整响应", newResp(100, ""), 100, false},
		{"被截断", newResp(100, ""), 40, true},
		{"写出多于声明(不判截断)", newResp(100, ""), 120, false},
		{"分块传输无法校验", newResp(-1, ""), 40, false},
		{"空响应体", newResp(0, ""), 0, false},
		{"压缩响应不可比", newResp(100, "gzip"), 40, false},
		{"resp 为空", nil, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkNonStreamTruncated(tc.resp, tc.written)
			if (err != nil) != tc.wantError {
				t.Errorf("checkNonStreamTruncated(written=%d) 错误 = %v, 期望有错误 = %v",
					tc.written, err, tc.wantError)
			}
		})
	}
}

// TestBoundAddressesReflectsActualListeners 监听地址在启动时冻结，
// UI 展示必须以实际绑定为准。
func TestBoundAddressesReflectsActualListeners(t *testing.T) {
	setupRenameTestEnv(t)

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("探测空闲端口失败: %v", err)
	}
	primary := probe.Addr().String()
	_ = probe.Close()

	prs := newTestRelayService(NewProviderService())
	prs.addr = primary

	if got := prs.BoundAddresses(); len(got) != 0 {
		t.Errorf("未启动时不应有绑定地址，实际 %v", got)
	}

	if err := prs.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer prs.Stop()

	bound := prs.BoundAddresses()
	if len(bound) != 1 || bound[0] != primary {
		t.Errorf("BoundAddresses() = %v, want [%s]", bound, primary)
	}

	// 返回的必须是副本，外部改动不能影响内部状态
	bound[0] = "tampered"
	if again := prs.BoundAddresses(); again[0] != primary {
		t.Errorf("BoundAddresses 返回了内部切片，被外部改坏: %v", again)
	}
}

// TestBoundAddressesClearedAfterStop 停掉之后再对外报告"正在监听 xxx"
// 会误导 UI。
func TestBoundAddressesClearedAfterStop(t *testing.T) {
	setupRenameTestEnv(t)

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("探测空闲端口失败: %v", err)
	}
	primary := probe.Addr().String()
	_ = probe.Close()

	prs := newTestRelayService(NewProviderService())
	prs.addr = primary

	if err := prs.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	if len(prs.BoundAddresses()) != 1 {
		t.Fatalf("启动后应有 1 个绑定地址，实际 %v", prs.BoundAddresses())
	}

	if err := prs.Stop(); err != nil {
		t.Fatalf("停止失败: %v", err)
	}
	if got := prs.BoundAddresses(); len(got) != 0 {
		t.Errorf("停止后不应再报告绑定地址，实际 %v", got)
	}
}

// respondNoEligibleProviders 按跳过原因分支输出可操作的排查文案（issue #29）
func TestRespondNoEligibleProvidersBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	run := func(model string, b, i int) string {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		respondNoEligibleProviders(c, model, b, i)
		if w.Code != http.StatusNotFound {
			t.Fatalf("应为 404, 实际 %d", w.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		msg, _ := body["error"].(string)
		return msg
	}
	if msg := run("", 3, 0); !strings.Contains(msg, "拉黑") || !strings.Contains(msg, "黑名单页") {
		t.Errorf("拉黑分支文案缺要素: %s", msg)
	}
	// 混合原因必须全部列出，不得只挑一个当代表（否则"都被拉黑"会掩盖校验失败）
	if msg := run("", 2, 1); !strings.Contains(msg, "拉黑") || !strings.Contains(msg, "配置校验") {
		t.Errorf("拉黑+校验失败组合应同时列出两种原因: %s", msg)
	}
	if msg := run("", 0, 2); !strings.Contains(msg, "配置校验") {
		t.Errorf("校验失败分支文案缺要素: %s", msg)
	}
	if msg := run("", 0, 0); !strings.Contains(msg, "没有已启用的供应商") {
		t.Errorf("空供应商分支文案缺要素: %s", msg)
	}
}
