package services

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	modelpricing "codeswitch/resources/model-pricing"
	"github.com/daodao97/xgo/xdb"
)

// TestDetermineStatusModelRejection 模型拒绝类 400/404 记为 validation_failed(不进拉黑计数)。
func TestDetermineStatusModelRejection(t *testing.T) {
	hcs := &HealthCheckService{}

	status, _ := hcs.determineStatus(400, 100, []byte(`{"error":{"message":"The model gpt-5.6 does not exist"}}`))
	if status != HealthStatusValidationError {
		t.Errorf("模型不存在的 400 应为 validation_failed,实际 %s", status)
	}

	status, _ = hcs.determineStatus(404, 100, []byte(`{"error":"model not found: gpt-5.5"}`))
	if status != HealthStatusValidationError {
		t.Errorf("模型不存在的 404 应为 validation_failed,实际 %s", status)
	}

	status, _ = hcs.determineStatus(400, 100, []byte(`{"error":"missing required field max_tokens"}`))
	if status != HealthStatusFailed {
		t.Errorf("普通 400 应保持 failed,实际 %s", status)
	}

	status, _ = hcs.determineStatus(404, 100, []byte(`<html>nginx 404</html>`))
	if status != HealthStatusFailed {
		t.Errorf("端点 404 应为 failed,实际 %s", status)
	}
}

// TestBuildTestRequestByEndpoint 所有 Codex 探测均使用 Responses 请求体。
func TestBuildTestRequestByEndpoint(t *testing.T) {
	hcs := &HealthCheckService{}

	decode := func(data []byte) map[string]any {
		payload := map[string]any{}
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return payload
	}

	payload := decode(hcs.buildTestRequest(CodexPlatform, "gpt-5.6", "/responses"))
	if payload["input"] != "ping" || payload["stream"] != false || payload["messages"] != nil {
		t.Errorf("/responses 应使用 Responses 协议体: %v", payload)
	}

	payload = decode(hcs.buildTestRequest(CodexPlatform, "gpt-5.6", "/v1/chat/completions"))
	if payload["input"] != "ping" || payload["messages"] != nil {
		t.Errorf("端点覆盖不应改变 Responses 请求体: %v", payload)
	}

	if body := hcs.buildTestRequest("removed", "gpt-5.6", "/responses"); body != nil {
		t.Errorf("已删除平台应返回 nil 请求体: %s", body)
	}
}

// TestGetEffectiveModelIsStableAcrossCatalogSync 验证目录同步与白名单不能改变产品探测模型。
func TestGetEffectiveModelIsStableAcrossCatalogSync(t *testing.T) {
	pricing, err := modelpricing.NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	catalogs := fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-5.6": catalogModel("gpt-5.6", "2026-07-09"),
			"gpt-5.5": catalogModel("gpt-5.5", "2026-04-23"),
		}},
	}
	if _, err := pricing.Rebuild(modelpricing.ConvertCatalogs(catalogs)); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	fixed, _ := time.ParseInLocation("2006-01-02", "2026-09-01", time.UTC) // 5.6 已过稳定窗
	policy := &DefaultModelPolicy{pricing: pricing, source: catalogs, now: func() time.Time { return fixed }}
	hcs := &HealthCheckService{policy: policy}

	// 无白名单:固定产品探测值
	open := &Provider{}
	if got := hcs.getEffectiveModel(open, "codex"); got != "gpt-5.6-sol" {
		t.Errorf("无白名单应取固定值 gpt-5.6-sol,实际 %s", got)
	}

	// 白名单不含固定值时仍保持固定值，由模型拒绝分类避免误拉黑。
	limited := &Provider{SupportedModels: map[string]bool{"gpt-5.5": true}}
	if got := hcs.getEffectiveModel(limited, "codex"); got != "gpt-5.6-sol" {
		t.Errorf("白名单不应改变固定探测值,实际 %s", got)
	}

	// 用户配置最高优先
	custom := &Provider{AvailabilityConfig: &AvailabilityConfig{TestModel: "my-model"}}
	if got := hcs.getEffectiveModel(custom, "codex"); got != "my-model" {
		t.Errorf("用户配置应最高优先,实际 %s", got)
	}
}

// TestIsModelRejectionBody 关键词识别边界。
func TestIsModelRejectionBody(t *testing.T) {
	if isModelRejectionBody([]byte(`{"error":"rate limit exceeded"}`)) {
		t.Error("非模型错误不应命中")
	}
	if !isModelRejectionBody([]byte(`{"message":"不支持的模型: glm-5"}`)) {
		t.Error("中文模型拒绝应命中")
	}
	if isModelRejectionBody([]byte(strings.Repeat("x", 10))) {
		t.Error("无关文本不应命中")
	}
}

func TestHealthHistoryIsCodexOnly(t *testing.T) {
	setupBlacklistFixEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{CodexPlatform, "claude"} {
		if _, err := db.Exec(`INSERT INTO health_check_history
			(provider_id, provider_name, platform, status, checked_at)
			VALUES (1, 'mixed', ?, ?, datetime('now', '-10 days'))`, platform, HealthStatusOperational); err != nil {
			t.Fatalf("写入 %s 历史失败: %v", platform, err)
		}
	}

	hcs := NewHealthCheckService(NewProviderService(), nil, nil, nil)
	history, err := hcs.GetHistory(" CODEX ", "mixed", 10)
	if err != nil {
		t.Fatalf("规范化 Codex 查询失败: %v", err)
	}
	if len(history.Items) != 1 || history.Platform != CodexPlatform {
		t.Fatalf("Codex 历史 = %+v, want 1 条且平台为 codex", history)
	}
	if _, err := hcs.GetHistory("claude", "mixed", 10); err == nil {
		t.Fatal("已删除平台的健康历史查询应被拒绝")
	}

	result := &HealthCheckResult{
		ProviderID: "1", ProviderName: "mixed", Platform: " CODEX ",
		Status: HealthStatusOperational, CheckedAt: time.Now(),
	}
	if err := hcs.saveResult(result); err != nil {
		t.Fatalf("保存规范化健康结果失败: %v", err)
	}
	var storedPlatform string
	if err := db.QueryRow(`SELECT platform FROM health_check_history ORDER BY id DESC LIMIT 1`).Scan(&storedPlatform); err != nil {
		t.Fatal(err)
	}
	if storedPlatform != CodexPlatform {
		t.Fatalf("健康结果平台 = %q, want %q", storedPlatform, CodexPlatform)
	}

	affected, err := hcs.CleanupOldRecords(1)
	if err != nil {
		t.Fatalf("清理历史失败: %v", err)
	}
	if affected != 1 {
		t.Fatalf("清理行数 = %d, want 1 条 Codex 记录", affected)
	}
	var removedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM health_check_history WHERE platform = 'claude'`).Scan(&removedCount); err != nil {
		t.Fatal(err)
	}
	if removedCount != 1 {
		t.Fatalf("旧平台健康历史被改写或删除,剩余 %d 条", removedCount)
	}
}
