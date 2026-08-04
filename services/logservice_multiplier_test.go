package services

import (
	"testing"
	"time"

	modelpricing "codeswitch/resources/model-pricing"

	"github.com/daodao97/xgo/xdb"
)

func TestApplyCostMultiplierScalesAllAmounts(t *testing.T) {
	base := modelpricing.CostBreakdown{
		InputCost:       1,
		OutputCost:      2,
		ReasoningCost:   3,
		CacheCreateCost: 4,
		CacheReadCost:   5,
		TotalCost:       15,
		HasPricing:      true,
		IsLongContext:   true,
		IsTiered:        true,
	}

	got := applyCostMultiplier(base, 1.5)
	if got.InputCost != 1.5 || got.OutputCost != 3 || got.ReasoningCost != 4.5 ||
		got.CacheCreateCost != 6 || got.CacheReadCost != 7.5 || got.TotalCost != 22.5 {
		t.Fatalf("unexpected scaled cost: %+v", got)
	}
	if !got.HasPricing || !got.IsLongContext || !got.IsTiered {
		t.Fatalf("pricing metadata should remain unchanged: %+v", got)
	}
}

func TestLogServiceAppliesCurrentProviderMultiplierToHistory(t *testing.T) {
	setupRenameTestEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("get database: %v", err)
	}
	if err := ensureRequestLogTableWithDB(db); err != nil {
		t.Fatalf("complete request log schema: %v", err)
	}

	providerService := NewProviderService()
	provider := Provider{ID: 1, Name: "priced", CostMultiplier: 2}
	saveProviderFixture(t, providerService, []Provider{provider})
	if _, err := db.Exec(
		`INSERT INTO provider_alias (platform, provider_id, alias_name, canonical_name, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		CodexPlatform, provider.ID, "priced-old", provider.Name,
		time.Now().Add(time.Hour).UTC().Format(timeLayout),
	); err != nil {
		t.Fatalf("insert provider alias: %v", err)
	}

	createdAt := time.Now().UTC().Format(timeLayout)
	if _, err := db.Exec(
		`INSERT INTO request_log
		 (platform, model, provider, http_code, input_tokens, output_tokens,
		  cache_read_tokens, reasoning_tokens, created_at)
		 VALUES (?, ?, ?, 200, 1000, 100, 20, 10, ?)`,
		CodexPlatform, "gpt-5", "priced-old", createdAt,
	); err != nil {
		t.Fatalf("insert request log: %v", err)
	}

	logService := NewLogService(providerService)
	usage := modelpricing.UsageSnapshot{
		InputTokens: 1000, OutputTokens: 100, CacheReadTokens: 20, ReasoningTokens: 10,
	}
	base := logService.pricing.CalculateCost("gpt-5", usage)
	assertReportMultiplier(t, logService, base.TotalCost*2)

	provider.CostMultiplier = 0.5
	saveProviderFixture(t, providerService, []Provider{provider})
	assertReportMultiplier(t, logService, base.TotalCost*0.5)

	saveProviderFixture(t, providerService, nil)
	start := time.Now().Add(-time.Hour).Format(time.RFC3339)
	total, err := logService.CostSince(start, CodexPlatform)
	if err != nil {
		t.Fatalf("CostSince without current provider: %v", err)
	}
	assertFloatClose(t, total, base.TotalCost)
}

func assertReportMultiplier(t *testing.T, logService *LogService, want float64) {
	t.Helper()
	start := time.Now().Add(-time.Hour).Format(time.RFC3339)

	total, err := logService.CostSince(start, CodexPlatform)
	if err != nil {
		t.Fatalf("CostSince: %v", err)
	}
	assertFloatClose(t, total, want)

	logs, err := logService.ListRequestLogs(CodexPlatform, "", 10)
	if err != nil {
		t.Fatalf("ListRequestLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("ListRequestLogs returned %d rows, want 1", len(logs))
	}
	assertFloatClose(t, logs[0].TotalCost, want)

	heatmap, err := logService.HeatmapStats(1)
	if err != nil {
		t.Fatalf("HeatmapStats: %v", err)
	}
	heatmapTotal := 0.0
	for _, bucket := range heatmap {
		heatmapTotal += bucket.TotalCost
	}
	assertFloatClose(t, heatmapTotal, want)

	stats, err := logService.StatsSince(CodexPlatform)
	if err != nil {
		t.Fatalf("StatsSince: %v", err)
	}
	assertFloatClose(t, stats.CostTotal, want)

	providerStats, err := logService.ProviderDailyStats(CodexPlatform)
	if err != nil {
		t.Fatalf("ProviderDailyStats: %v", err)
	}
	if len(providerStats) != 1 {
		t.Fatalf("ProviderDailyStats returned %d rows, want 1", len(providerStats))
	}
	assertFloatClose(t, providerStats[0].CostTotal, want)
}

func assertFloatClose(t *testing.T, got, want float64) {
	t.Helper()
	const tolerance = 1e-12
	if got < want-tolerance || got > want+tolerance {
		t.Fatalf("got %.15f, want %.15f", got, want)
	}
}
