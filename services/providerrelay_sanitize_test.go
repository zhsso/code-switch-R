package services

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

// slp 构造 *[]string 测试字面量；slp() 表示"显式空数组 = 什么都不删"。
func slp(items ...string) *[]string {
	v := append([]string{}, items...)
	return &v
}

// —— sanitizeRequestBody ——

func TestSanitizeRequestBodyDefaults(t *testing.T) {
	body := []byte(`{"model":"m1","prompt_caching":{"enabled":true},"messages":[{"role":"user","content":"hi"}]}`)
	cleaned, removed := sanitizeRequestBody(body, nil)

	if !reflect.DeepEqual(removed, []string{"prompt_caching"}) {
		t.Fatalf("expected to remove prompt_caching, got %v", removed)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &m); err != nil {
		t.Fatalf("cleaned body is not valid JSON: %v", err)
	}
	if _, ok := m["prompt_caching"]; ok {
		t.Fatal("prompt_caching should be removed")
	}
	// 非目标字段必须逐字节保留
	if string(m["messages"]) != `[{"role":"user","content":"hi"}]` {
		t.Fatalf("messages value changed: %s", m["messages"])
	}
	if string(m["model"]) != `"m1"` {
		t.Fatalf("model value changed: %s", m["model"])
	}
}

func TestSanitizeRequestBodyCustomList(t *testing.T) {
	cfg := &SanitizeConfig{BlockedBodyFields: slp("foo", "bar")}
	body := []byte(`{"foo":1,"bar":2,"baz":3,"prompt_caching":true}`)
	cleaned, removed := sanitizeRequestBody(body, cfg)

	if !reflect.DeepEqual(removed, []string{"bar", "foo"}) {
		t.Fatalf("expected [bar foo] removed (sorted), got %v", removed)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &m); err != nil {
		t.Fatal(err)
	}
	// 自定义列表生效时不再叠加内置默认：prompt_caching 保留
	if _, ok := m["prompt_caching"]; !ok {
		t.Fatal("custom list should fully replace defaults; prompt_caching must stay")
	}
	if _, ok := m["baz"]; !ok {
		t.Fatal("baz should stay")
	}
}

// 三态语义：nil = 用内置默认；空数组 = 什么都不删。
func TestSanitizeRequestBodyTriState(t *testing.T) {
	body := []byte(`{"prompt_caching":true,"x":1}`)

	// nil 列表 → 默认黑名单生效
	cleanedNil, removedNil := sanitizeRequestBody(body, &SanitizeConfig{})
	if len(removedNil) != 1 || removedNil[0] != "prompt_caching" {
		t.Fatalf("nil list should fall back to defaults, removed=%v", removedNil)
	}
	_ = cleanedNil

	// 显式空数组 → 不删任何字段
	cleanedEmpty, removedEmpty := sanitizeRequestBody(body, &SanitizeConfig{BlockedBodyFields: slp()})
	if len(removedEmpty) != 0 {
		t.Fatalf("empty list means remove nothing, removed=%v", removedEmpty)
	}
	if string(cleanedEmpty) != string(body) {
		t.Fatal("body must be untouched when list is explicitly empty")
	}
}

// 含点号的顶层键按字面量删除，不能当作嵌套路径误删。
func TestSanitizeRequestBodyDottedKey(t *testing.T) {
	cfg := &SanitizeConfig{BlockedBodyFields: slp("a.b")}
	body := []byte(`{"a.b":1,"a":{"b":2}}`)
	cleaned, removed := sanitizeRequestBody(body, cfg)

	if !reflect.DeepEqual(removed, []string{"a.b"}) {
		t.Fatalf("expected literal key a.b removed, got %v", removed)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["a.b"]; ok {
		t.Fatal("literal a.b should be removed")
	}
	if string(m["a"]) != `{"b":2}` {
		t.Fatalf("nested a.b must not be touched, got %s", m["a"])
	}
}

// 顶层重复键的畸形 body 原样放行，不做任何改写。
func TestSanitizeRequestBodyDuplicateKeysPassthrough(t *testing.T) {
	body := []byte(`{"prompt_caching":1,"prompt_caching":2,"x":3}`)
	cleaned, removed := sanitizeRequestBody(body, nil)
	if len(removed) != 0 {
		t.Fatalf("duplicate-key body must pass through, removed=%v", removed)
	}
	if string(cleaned) != string(body) {
		t.Fatal("duplicate-key body must be byte-identical")
	}
}

func TestSanitizeRequestBodyNonObjectAndInvalid(t *testing.T) {
	for _, body := range []string{`[1,2,3]`, `"str"`, `not-json`} {
		cleaned, removed := sanitizeRequestBody([]byte(body), nil)
		if len(removed) != 0 || string(cleaned) != body {
			t.Fatalf("non-object body %q must pass through untouched", body)
		}
	}
}

func TestSanitizeRequestBodyNoMatchFastPath(t *testing.T) {
	body := []byte(`{"model":"m","messages":[]}`)
	cleaned, removed := sanitizeRequestBody(body, nil)
	if len(removed) != 0 {
		t.Fatalf("nothing should be removed, got %v", removed)
	}
	if &cleaned[0] != &body[0] {
		t.Fatal("fast path should return the original slice without rebuild")
	}
}

// —— sanitizeHeaders ——

func TestSanitizeHeadersBlocked(t *testing.T) {
	cfg := &SanitizeConfig{BlockedHeaders: slp("x-custom-junk")}
	headers := map[string]string{
		"X-Custom-Junk": "v", // 大小写不敏感命中
		"Content-Type":  "application/json",
	}
	cleaned := sanitizeHeaders(headers, cfg)

	if _, ok := cleaned["X-Custom-Junk"]; ok {
		t.Fatal("blocked header should be removed case-insensitively")
	}
	if cleaned["Content-Type"] != "application/json" {
		t.Fatal("unrelated header must stay")
	}
}

func TestSanitizeHeadersPreservesUnblocked(t *testing.T) {
	headers := map[string]string{"X-Allowed": "value"}
	cleaned := sanitizeHeaders(headers, nil)
	if cleaned["X-Allowed"] != "value" {
		t.Fatalf("unblocked header changed: %q", cleaned["X-Allowed"])
	}
}

func TestSanitizeHeadersEmptyListMeansKeepAll(t *testing.T) {
	cfg := &SanitizeConfig{BlockedHeaders: slp()}
	headers := map[string]string{"X-Allowed": "value"}
	cleaned := sanitizeHeaders(headers, cfg)
	if cleaned["X-Allowed"] != "value" {
		t.Fatal("explicit empty header blocklist means keep everything")
	}
}

func TestResolveBlocklistIgnoresEmptyValues(t *testing.T) {
	blocked := resolveBlocklist([]string{"x-test", "  "}, nil, true)
	if !blocked["x-test"] || len(blocked) != 1 {
		t.Fatalf("unexpected blocklist: %v", blocked)
	}
}

func TestSanitizeHTTPHeaders(t *testing.T) {
	cfg := &SanitizeConfig{BlockedHeaders: slp("x-stainless-lang")}
	h := http.Header{}
	h.Set("X-Stainless-Lang", "go")
	h.Set("X-Allowed", "value")
	h.Set("Accept", "application/json")

	sanitizeHTTPHeaders(h, cfg)

	if h.Get("X-Stainless-Lang") != "" {
		t.Fatal("blocked header should be deleted")
	}
	if h.Get("X-Allowed") != "value" {
		t.Fatalf("unblocked header changed: %q", h.Get("X-Allowed"))
	}
	if h.Get("Accept") != "application/json" {
		t.Fatal("unrelated header must stay")
	}
}

func TestSanitizeHTTPHeadersMultiValuePreserved(t *testing.T) {
	h := http.Header{}
	h.Add("X-Allowed", "value-1")
	h.Add("X-Allowed", "value-2")

	sanitizeHTTPHeaders(h, nil)

	vals := h.Values("X-Allowed")
	if len(vals) != 2 || vals[0] != "value-1" || vals[1] != "value-2" {
		t.Fatalf("unblocked multi-value header changed: %v", vals)
	}
}

// —— 端到端出站捕获：经 forwardRequest 实际转发，验证清理作用于真实出站请求 ——

func TestForwardRequestSanitizesOutbound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = setupRenameTestEnv(t)

	type captured struct {
		body []byte
		junk []string
		auth []string
	}
	var got captured

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.body, _ = io.ReadAll(r.Body)
		got.junk = r.Header.Values("X-Junk")
		got.auth = r.Header.Values("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	prs := newTestRelayService(NewProviderService())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"gpt-5.6","prompt_caching":true,"input":[]}`)
	req, err := http.NewRequest("POST", "/responses", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("X-Junk", "should-be-removed")
	c.Request = req

	provider := Provider{
		Name:                   "sanitize-me",
		APIURL:                 upstream.URL,
		APIKey:                 "provider-secret",
		Enabled:                true,
		ConnectivityAuthType:   "bearer",
		RequestSanitizeEnabled: true,
		SanitizeConfig:         &SanitizeConfig{BlockedHeaders: slp("x-junk")},
	}
	ok, ferr := prs.forwardRequest(c, CodexPlatform, provider, "/responses",
		map[string]string{}, cloneHeaders(req.Header), body, false, "gpt-5.6", 0)
	if !ok {
		t.Fatalf("转发应成功,实际失败: %v", ferr)
	}

	// 请求体：默认黑名单删掉 prompt_caching，其余保留
	var outBody map[string]json.RawMessage
	if err := json.Unmarshal(got.body, &outBody); err != nil {
		t.Fatalf("出站 body 不是合法 JSON: %v", err)
	}
	if _, exists := outBody["prompt_caching"]; exists {
		t.Error("出站 body 应已删除 prompt_caching")
	}
	if string(outBody["model"]) != `"gpt-5.6"` {
		t.Errorf("出站 body 的 model 被改动: %s", outBody["model"])
	}

	// 请求头：自定义黑名单删 x-junk；注入的凭据不受清理影响。
	if len(got.junk) != 0 {
		t.Errorf("出站不应携带 X-Junk,实际 %v", got.junk)
	}
	if len(got.auth) != 1 || got.auth[0] != "Bearer provider-secret" {
		t.Errorf("供应商凭据应完好注入,实际 %v", got.auth)
	}
}

// 复制供应商必须带上 TLS 跳验与请求清理配置，且 SanitizeConfig 为深拷贝。
func TestDuplicateProviderCopiesTLSAndSanitize(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()

	source := Provider{
		ID:                     1,
		Name:                   "src",
		APIURL:                 "https://api.example.com",
		APIKey:                 "sk",
		Enabled:                true,
		InsecureSkipVerify:     true,
		RequestSanitizeEnabled: true,
		SanitizeConfig: &SanitizeConfig{
			BlockedBodyFields: slp("foo"),
			BlockedHeaders:    slp(),
		},
	}
	if err := ps.SaveProviders(CodexPlatform, []Provider{source}); err != nil {
		t.Fatalf("保存夹具失败: %v", err)
	}

	cloned, err := ps.DuplicateProvider(CodexPlatform, source.ID)
	if err != nil {
		t.Fatalf("DuplicateProvider 失败: %v", err)
	}

	if !cloned.InsecureSkipVerify {
		t.Error("副本应保留 InsecureSkipVerify")
	}
	if !cloned.RequestSanitizeEnabled {
		t.Error("副本应保留 RequestSanitizeEnabled")
	}
	if cloned.SanitizeConfig == nil {
		t.Fatal("副本应保留 SanitizeConfig")
	}
	if got := cfgBodyFields(cloned.SanitizeConfig); len(got) != 1 || got[0] != "foo" {
		t.Errorf("BlockedBodyFields 复制不完整: %v", got)
	}
	// 三态：显式空数组保持为空数组指针，不能退化成 nil（= 用默认）
	if cloned.SanitizeConfig.BlockedHeaders == nil || len(*cloned.SanitizeConfig.BlockedHeaders) != 0 {
		t.Errorf("显式空 BlockedHeaders 应保持空数组指针: %v", cloned.SanitizeConfig.BlockedHeaders)
	}
	// 深拷贝：改副本不影响源
	(*cloned.SanitizeConfig.BlockedBodyFields)[0] = "mutated"
	if (*source.SanitizeConfig.BlockedBodyFields)[0] != "foo" {
		t.Error("SanitizeConfig 应为深拷贝，副本修改不能影响源")
	}
}
