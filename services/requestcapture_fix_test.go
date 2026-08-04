package services

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daodao97/xgo/xdb"
	"github.com/gin-gonic/gin"
)

func TestTruncateUTF8(t *testing.T) {
	s := strings.Repeat("汉", 10) // 30 字节
	got := truncateUTF8(s, 10)   // 10 不是 3 的倍数,必须回退到字符边界
	if len(got) > 10 {
		t.Errorf("超过限额: %d", len(got))
	}
	if !strings.HasPrefix(s, got) || len(got)%3 != 0 {
		t.Errorf("截断打断了多字节序列: %q", got)
	}
	if truncateUTF8("abc", 10) != "abc" {
		t.Error("限额内应原样返回")
	}
}

// ==================== 迁移 ====================

// 抓包列在旧库上的迁移必须幂等,且新库建表即包含
func TestRequestLogCaptureColumnsMigration(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "capture.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	// 模拟旧库:无抓包列
	if _, err := db.Exec(`CREATE TABLE request_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT, platform TEXT, model TEXT, provider TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("建旧表失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO request_log (platform) VALUES ('codex')`); err != nil {
		t.Fatalf("写历史数据失败: %v", err)
	}

	for i := 0; i < 2; i++ { // 跑两遍验证幂等
		if err := ensureRequestLogTableWithDB(db); err != nil {
			t.Fatalf("第 %d 次迁移失败: %v", i+1, err)
		}
	}
	for _, col := range []string{"request_url", "request_headers", "request_body", "body_truncated", "body_bytes",
		"response_headers", "response_body", "response_truncated", "response_bytes", "budget_skipped", "capture_session_id"} {
		exists, err := requestLogColumnExists(db, col)
		if err != nil {
			t.Fatalf("查询列 %s 失败: %v", col, err)
		}
		if !exists {
			t.Errorf("迁移后缺列 %s", col)
		}
	}
	// 历史行的新列必须落在默认值上,清空/详情逻辑才不会被 NULL 干扰
	var headers, body string
	var truncated, bytes int
	if err := db.QueryRow(`SELECT request_headers, request_body, body_truncated, body_bytes FROM request_log WHERE id = 1`).
		Scan(&headers, &body, &truncated, &bytes); err != nil {
		t.Fatalf("历史行新列读取失败(可能为 NULL): %v", err)
	}
	if headers != "" || body != "" || truncated != 0 || bytes != 0 {
		t.Errorf("历史行默认值错误: %q %q %d %d", headers, body, truncated, bytes)
	}
}

// ==================== 真实 INSERT 路径 ====================

// setupCaptureDBEnv 在隔离环境上把 request_log 升级到含抓包列的完整结构,
// 并挂载真实批量写入队列(ExecBatchCtx 同步等待提交,转发返回即已落库)
func setupCaptureDBEnv(t *testing.T) *sql.DB {
	t.Helper()
	setupRenameTestEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取数据库失败: %v", err)
	}
	if err := ensureRequestLogTableWithDB(db); err != nil {
		t.Fatalf("升级 request_log 失败: %v", err)
	}
	oldQueue := GlobalDBQueueLogs
	GlobalDBQueueLogs = NewDBWriteQueue(db, 100, true)
	t.Cleanup(func() {
		_ = GlobalDBQueueLogs.Shutdown(3 * time.Second)
		GlobalDBQueueLogs = oldQueue
	})
	return db
}

// Codex 转发路径：开关开启时落库终态请求，关闭时抓包列为空。
func TestForwardRequestCaptureOnOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupCaptureDBEnv(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	prs := newTestRelayService(NewProviderService())
	provider := Provider{
		ID: 1, Name: "cap-p", APIURL: upstream.URL, APIKey: "provider-secret",
		Enabled: true, ConnectivityAuthType: "X-Secret-Auth", // 自定义认证头名
	}
	body := []byte(`{"model":"m","api_key":"provider-secret","messages":[{"role":"user","content":"hi"}]}`)

	send := func() {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		req, _ := http.NewRequest("POST", "/responses", strings.NewReader(string(body)))
		c.Request = req
		ok, ferr := prs.forwardRequest(c, CodexPlatform, provider, "/responses",
			map[string]string{}, map[string]string{"Content-Type": "application/json"}, body, false, "m", 0)
		if !ok {
			t.Fatalf("转发应成功: %v", ferr)
		}
	}

	prs.SetRequestCapture(true)
	if !prs.GetRequestCapture() {
		t.Fatal("开关读写不一致")
	}
	send()
	prs.SetRequestCapture(false)
	send()

	rows, err := db.Query(`SELECT id, request_headers, request_body, body_bytes FROM request_log ORDER BY id`)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	defer rows.Close()
	type row struct {
		id        int64
		headers   string
		body      string
		bodyBytes int
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.headers, &r.body, &r.bodyBytes); err != nil {
			t.Fatalf("扫描失败: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("应有 2 条日志, 实际 %d", len(got))
	}

	// 第一条:开关开启（全量不脱敏）
	on := got[0]
	if on.headers == "" || on.body == "" || on.bodyBytes != len(body) {
		t.Fatalf("开启时应录制: headers=%q body=%q bytes=%d", on.headers, on.body, on.bodyBytes)
	}
	var hm map[string]interface{}
	if err := json.Unmarshal([]byte(on.headers), &hm); err != nil {
		t.Fatalf("落库请求头必须是合法 JSON: %v", err)
	}
	// 全量不脱敏：请求体原样保留，密钥不打码
	if !strings.Contains(on.body, "provider-secret") {
		t.Errorf("全量模式请求体应原样保留密钥（不脱敏）: %s", on.body)
	}
	if !strings.Contains(on.body, `"messages"`) {
		t.Errorf("正文内容应完整保留: %s", on.body)
	}

	// 第二条:开关关闭
	off := got[1]
	if off.headers != "" || off.body != "" || off.bodyBytes != 0 {
		t.Errorf("关闭时不应录制: %+v", off)
	}

	// 列表接口:has_capture 计算列区分两行,序列化不携带大字段
	ls := NewLogService()
	logs, err := ls.ListRequestLogs("codex", "", 10)
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("列表应有 2 条, 实际 %d", len(logs))
	}
	// 列表按 id 倒序:第 0 条是关闭的那次
	if logs[0].HasCapture || !logs[1].HasCapture {
		t.Errorf("has_capture 标记错误: %v %v", logs[0].HasCapture, logs[1].HasCapture)
	}
	data, _ := json.Marshal(logs[1])
	if strings.Contains(string(data), "request_body") || strings.Contains(string(data), `"messages"`) {
		t.Errorf("列表序列化不应携带抓包大字段: %s", data)
	}

	// 详情接口:返回抓包内容（含响应），并录到上游响应体
	detail, err := ls.GetRequestLogDetail(on.id)
	if err != nil {
		t.Fatalf("详情查询失败: %v", err)
	}
	if detail.RequestHeaders != on.headers || detail.RequestBody != on.body || detail.BodyBytes != len(body) {
		t.Errorf("详情与落库请求内容不一致")
	}
	if !strings.Contains(detail.ResponseBody, `"ok":true`) {
		t.Errorf("应录到上游响应体, 实际 %q", detail.ResponseBody)
	}
	if _, err := ls.GetRequestLogDetail(99999); err == nil {
		t.Error("不存在的 ID 应报错")
	}

	// 清除:只清抓包列,统计行保留
	affected, err := prs.ClearCapturedRequests()
	if err != nil {
		t.Fatalf("清除失败: %v", err)
	}
	if affected != 1 {
		t.Errorf("应清理 1 行, 实际 %d", affected)
	}
	var cnt int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_log WHERE ` + captureRowPredicate).Scan(&cnt); err != nil {
		t.Fatalf("复查失败: %v", err)
	}
	if cnt != 0 {
		t.Errorf("清除后仍有 %d 行残留抓包数据", cnt)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_log`).Scan(&cnt); err != nil || cnt != 2 {
		t.Errorf("统计行不应被删除: cnt=%d err=%v", cnt, err)
	}
}
