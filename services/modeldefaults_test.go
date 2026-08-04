package services

import (
	"testing"
	"time"

	modelpricing "codeswitch/resources/model-pricing"
)

type fakeCatalogSource map[string]*modelpricing.RemoteCatalog

func (f fakeCatalogSource) Catalogs() map[string]*modelpricing.RemoteCatalog { return f }

func fptr(v float64) *float64 { return &v }
func bptr(v bool) *bool       { return &v }

func catalogModel(id, release string, opts ...func(*modelpricing.RemoteModel)) modelpricing.RemoteModel {
	m := modelpricing.RemoteModel{
		ID:          id,
		ReleaseDate: release,
		Cost:        &modelpricing.RemoteCost{Input: fptr(1), Output: fptr(2)},
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

func newTestPolicy(t *testing.T, now string, catalogs fakeCatalogSource) *DefaultModelPolicy {
	t.Helper()
	pricing, err := modelpricing.NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := pricing.Rebuild(modelpricing.ConvertCatalogs(catalogs)); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	fixed, err := time.ParseInLocation("2006-01-02", now, time.UTC)
	if err != nil {
		t.Fatalf("parse now: %v", err)
	}
	return &DefaultModelPolicy{
		pricing: pricing,
		source:  catalogs,
		now:     func() time.Time { return fixed },
	}
}

func TestCompareVersionSegments(t *testing.T) {
	cases := []struct {
		a, b []int
		want int
	}{
		{[]int{5, 10}, []int{5, 9}, 1},
		{[]int{5}, []int{5, 0}, 0},
		{[]int{4, 5}, []int{5}, -1},
		{[]int{3, 1}, []int{3}, 1},
	}
	for _, c := range cases {
		if got := compareVersionSegments(c.a, c.b); got != c.want {
			t.Errorf("compare(%v,%v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestCodexDefaultModel 主线与 codex 专线的取舍:专线版本 >= 主线才选专线。
func TestCodexDefaultModel(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-5.6":       catalogModel("gpt-5.6", "2026-07-09"),
			"gpt-5.5":       catalogModel("gpt-5.5", "2026-04-23"),
			"gpt-5.3-codex": catalogModel("gpt-5.3-codex", "2026-02-05"),
			"gpt-5.6-luna":  catalogModel("gpt-5.6-luna", "2026-07-09"), // 变体不参与主线竞争
		}},
	})
	if got := policy.CodexDefaultModel(); got != "gpt-5.6" {
		t.Errorf("codex 默认 = %s, want gpt-5.6 (专线 5.3 < 主线 5.6)", got)
	}

	policy2 := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-5.6":       catalogModel("gpt-5.6", "2026-07-09"),
			"gpt-5.6-codex": catalogModel("gpt-5.6-codex", "2026-07-10"),
		}},
	})
	if got := policy2.CodexDefaultModel(); got != "gpt-5.6-codex" {
		t.Errorf("codex 默认 = %s, want gpt-5.6-codex (专线版本持平应选专线)", got)
	}
}

// TestCodexDefaultRequiresToolCall tool_call 显式 false 的模型不作产品默认;缺失不排除。
func TestCodexDefaultRequiresToolCall(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-7":   catalogModel("gpt-7", "2026-06-01", func(m *modelpricing.RemoteModel) { m.ToolCall = bptr(false) }),
			"gpt-5.6": catalogModel("gpt-5.6", "2026-07-09", func(m *modelpricing.RemoteModel) { m.ToolCall = bptr(true) }),
			"gpt-5.5": catalogModel("gpt-5.5", "2026-04-23"), // tool_call 缺失
		}},
	})
	if got := policy.CodexDefaultModel(); got != "gpt-5.6" {
		t.Errorf("codex 默认 = %s, want gpt-5.6 (gpt-7 tool_call=false 应排除)", got)
	}
}

// TestResolverExcludesFutureAndMissingRelease 未来日期与缺失 release_date 不入选。
func TestResolverExcludesFutureAndMissingRelease(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-9":   catalogModel("gpt-9", "2026-09-01"), // 未来
			"gpt-8":   catalogModel("gpt-8", ""),           // 缺失
			"gpt-5.6": catalogModel("gpt-5.6", "2026-07-09"),
		}},
	})
	if got := policy.CodexDefaultModel(); got != "gpt-5.6" {
		t.Errorf("codex 默认 = %s, want gpt-5.6 (未来/缺失日期应排除)", got)
	}
}

// TestPolicyFallbacksWithoutSource 无目录源时回退静态兜底。
func TestPolicyFallbacksWithoutSource(t *testing.T) {
	policy := NewDefaultModelPolicy()
	if got := policy.CodexDefaultModel(); got != FallbackCodexDefaultModel {
		t.Errorf("无源 codex 默认 = %s, want %s", got, FallbackCodexDefaultModel)
	}
	if got := policy.ProbeModel(CodexPlatform); got != FallbackCodexProbeModel {
		t.Errorf("无源 codex 探测 = %s, want %s", got, FallbackCodexProbeModel)
	}
	for _, platform := range []string{"claude", "gemini", ""} {
		if got := policy.ProbeModel(platform); got != "" {
			t.Errorf("ProbeModel(%q) = %q, want empty", platform, got)
		}
	}
}

func TestProbeModelCannotBeChangedByCatalogSync(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-99": catalogModel("gpt-99", "2026-07-01"),
		}},
	})
	if got := policy.ProbeModel(CodexPlatform); got != "gpt-5.6-sol" {
		t.Fatalf("ProbeModel = %q, want fixed gpt-5.6-sol", got)
	}
	candidates := policy.ProbeCandidates(CodexPlatform)
	if len(candidates) != 1 || candidates[0] != "gpt-5.6-sol" {
		t.Fatalf("ProbeCandidates = %v, want [gpt-5.6-sol]", candidates)
	}
}

// TestResolverRequiresPositivePricing 目录有条目但价格表无价的候选被跳过。
func TestResolverRequiresPositivePricing(t *testing.T) {
	catalogs := fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-7":   catalogModel("gpt-7", "2026-06-01", func(m *modelpricing.RemoteModel) { m.Cost = nil }), // 无价
			"gpt-5.6": catalogModel("gpt-5.6", "2026-07-09"),
		}},
	}
	policy := newTestPolicy(t, "2026-07-28", catalogs)
	if got := policy.CodexDefaultModel(); got != "gpt-5.6" {
		t.Errorf("codex 默认 = %s, want gpt-5.6 (无价条目应跳过)", got)
	}
}
