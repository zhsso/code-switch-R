package services

import (
	"fmt"
	"testing"
	"time"

	modelpricing "codeswitch/resources/model-pricing"

	"github.com/daodao97/xgo/xdb"
)

func setupDailyLimitTestService(
	t *testing.T,
	provider Provider,
	now time.Time,
) (*DailyCostLimitService, *ProviderService, *AppSettingsService) {
	t.Helper()
	setupRenameTestEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("get test database: %v", err)
	}
	if err := ensureRequestLogTableWithDB(db); err != nil {
		t.Fatalf("complete request_log schema: %v", err)
	}
	if err := ensureDailyCostLimitTable(); err != nil {
		t.Fatalf("create daily limit table: %v", err)
	}

	providerService := NewProviderService()
	saveProviderFixture(t, providerService, []Provider{provider})
	appSettings, err := NewAppSettingsService()
	if err != nil {
		t.Fatalf("create app settings service: %v", err)
	}
	settings := appSettings.defaultSettings()
	settings.Timezone = "Asia/Shanghai"
	if _, err := appSettings.SaveAppSettings(settings); err != nil {
		t.Fatalf("save app settings: %v", err)
	}

	service := NewDailyCostLimitService(providerService, appSettings, NewLogService(providerService))
	service.now = func() time.Time { return now }
	return service, providerService, appSettings
}

func requireDailyStatus(t *testing.T, service *DailyCostLimitService, providerID int64) DailyCostLimitStatus {
	t.Helper()
	statuses, err := service.GetStatuses(CodexPlatform)
	if err != nil {
		t.Fatalf("get daily limit statuses: %v", err)
	}
	for _, status := range statuses {
		if status.ProviderID == providerID {
			return status
		}
	}
	t.Fatalf("daily limit status for provider %d not found", providerID)
	return DailyCostLimitStatus{}
}

func TestDailyCostLimitResetsAtConfiguredTimezoneMidnight(t *testing.T) {
	provider := Provider{
		ID: 1, Name: "limited", Enabled: true,
		DailyCostLimitEnabled: true, DailyCostLimitMicros: 10 * microsPerUSD,
	}
	now := time.Date(2026, 8, 5, 15, 59, 0, 0, time.UTC)
	service, providerService, _ := setupDailyLimitTestService(t, provider, now)

	if err := service.SetActualUsage(CodexPlatform, provider.ID, provider.DailyCostLimitMicros); err != nil {
		t.Fatalf("set actual usage: %v", err)
	}
	before := requireDailyStatus(t, service, provider.ID)
	if before.Day != "2026-08-05" || !before.Blocked {
		t.Fatalf("unexpected status before midnight: %+v", before)
	}

	service.now = func() time.Time {
		return time.Date(2026, 8, 5, 16, 1, 0, 0, time.UTC)
	}
	after := requireDailyStatus(t, service, provider.ID)
	if after.Day != "2026-08-06" || after.Blocked || after.UsedMicros != 0 || after.ManualAdjustmentMicros != 0 {
		t.Fatalf("unexpected status after midnight: %+v", after)
	}
	providers, err := providerService.LoadProviders(CodexPlatform)
	if err != nil {
		t.Fatalf("load providers: %v", err)
	}
	if len(providers) != 1 || !providers[0].Enabled {
		t.Fatalf("daily reset must not change Provider.Enabled: %+v", providers)
	}
}

func TestDailyCostLimitScopeUsesIANATimezoneAcrossDST(t *testing.T) {
	provider := Provider{
		ID: 5, Name: "dst", Enabled: true,
		DailyCostLimitEnabled: true, DailyCostLimitMicros: microsPerUSD,
	}
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	service, _, appSettings := setupDailyLimitTestService(t, provider, now)
	settings, err := appSettings.GetAppSettings()
	if err != nil {
		t.Fatalf("load app settings: %v", err)
	}
	settings.Timezone = "America/New_York"
	if _, err := appSettings.SaveAppSettings(settings); err != nil {
		t.Fatalf("save DST timezone: %v", err)
	}

	timezone, day, start, end, err := service.currentScope()
	if err != nil {
		t.Fatalf("get current scope: %v", err)
	}
	if timezone != "America/New_York" || day != "2026-03-08" {
		t.Fatalf("unexpected scope: timezone=%q day=%q", timezone, day)
	}
	if duration := end.Sub(start); duration != 23*time.Hour {
		t.Fatalf("DST transition day duration = %v, want 23h", duration)
	}
}

func TestDailyCostLimitInitialStateHonorsActiveBlacklistAt95Percent(t *testing.T) {
	usage := modelpricing.UsageSnapshot{InputTokens: 1_000_000}
	priced := NewLogService().pricing.CalculateCost("gpt-5", usage)
	usedMicros := costToMicros(priced.TotalCost)
	if usedMicros <= 0 {
		t.Fatal("gpt-5 test usage must have a positive price")
	}
	provider := Provider{
		ID: 6, Name: "already-blacklisted", Enabled: true,
		DailyCostLimitEnabled: true, DailyCostLimitMicros: usedMicros + 1,
	}
	now := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	service, _, _ := setupDailyLimitTestService(t, provider, now)

	settingsService := NewSettingsService()
	setAppSetting(t, "enable_blacklist", "true")
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("get test database: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO request_log (
			platform, model, provider, input_tokens, created_at
		) VALUES (?, ?, ?, ?, ?)`,
		CodexPlatform, "gpt-5", provider.Name, usage.InputTokens, now.Format(timeLayout)); err != nil {
		t.Fatalf("seed priced request log: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO provider_blacklist (
			platform, provider_name, blacklisted_at, blacklisted_until
		) VALUES (?, ?, ?, ?)`,
		CodexPlatform, provider.Name, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("seed active blacklist: %v", err)
	}
	service.SetBlacklistService(NewBlacklistService(settingsService, nil))

	status := requireDailyStatus(t, service, provider.ID)
	if status.SystemCostMicros != usedMicros || status.UsedMicros >= status.LimitMicros {
		t.Fatalf("test must start between 95%% and 100%%: %+v", status)
	}
	if !status.AutoBlocked {
		t.Fatalf("initial 95%% usage plus an active blacklist should auto block: %+v", status)
	}
}

func TestDailyCostLimitTemporaryUnblockWaitsForNewTrigger(t *testing.T) {
	provider := Provider{
		ID: 2, Name: "near-limit", Enabled: true,
		DailyCostLimitEnabled: true, DailyCostLimitMicros: microsPerUSD,
	}
	service, _, _ := setupDailyLimitTestService(
		t,
		provider,
		time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC),
	)

	if err := service.SetActualUsage(CodexPlatform, provider.ID, 950_000); err != nil {
		t.Fatalf("set actual usage: %v", err)
	}
	service.OnProviderBlacklisted(CodexPlatform, provider.Name)
	if status := requireDailyStatus(t, service, provider.ID); !status.AutoBlocked {
		t.Fatalf("95%% + blacklist should auto block: %+v", status)
	}

	if err := service.TemporaryUnblock(CodexPlatform, provider.ID); err != nil {
		t.Fatalf("temporary unblock: %v", err)
	}
	firstRead := requireDailyStatus(t, service, provider.ID)
	secondRead := requireDailyStatus(t, service, provider.ID)
	if firstRead.Blocked || secondRead.Blocked {
		t.Fatalf("existing blacklist must not immediately cancel temporary unblock: first=%+v second=%+v", firstRead, secondRead)
	}

	service.OnProviderBlacklisted(CodexPlatform, provider.Name)
	if status := requireDailyStatus(t, service, provider.ID); !status.AutoBlocked {
		t.Fatalf("a new matching blacklist transition should re-block: %+v", status)
	}
}

func TestDailyCostLimitSettlementReblocksAfterTemporaryUnblock(t *testing.T) {
	requestLog := &RequestLog{
		Platform:    CodexPlatform,
		Model:       "gpt-5",
		Provider:    "settlement",
		InputTokens: 1_000_000,
	}
	priced := NewLogService().pricing.CalculateCost(
		requestLog.Model,
		modelpricing.UsageSnapshot{InputTokens: requestLog.InputTokens},
	)
	requestCostMicros := costToMicros(priced.TotalCost)
	if requestCostMicros <= 0 {
		t.Fatal("gpt-5 test request must have a positive price")
	}
	provider := Provider{
		ID: 7, Name: requestLog.Provider, Enabled: true,
		DailyCostLimitEnabled: true, DailyCostLimitMicros: requestCostMicros,
	}
	service, _, _ := setupDailyLimitTestService(
		t,
		provider,
		time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC),
	)

	if err := service.SettleRequest(provider, requestLog); err != nil {
		t.Fatalf("settle request at quota: %v", err)
	}
	if status := requireDailyStatus(t, service, provider.ID); !status.AutoBlocked || status.UsedMicros != requestCostMicros {
		t.Fatalf("settlement at 100%% should auto block: %+v", status)
	}
	if err := service.TemporaryUnblock(CodexPlatform, provider.ID); err != nil {
		t.Fatalf("temporary unblock: %v", err)
	}
	if status := requireDailyStatus(t, service, provider.ID); status.Blocked {
		t.Fatalf("temporary unblock should clear the current gate: %+v", status)
	}

	if err := service.SettleRequest(provider, requestLog); err != nil {
		t.Fatalf("settle request after temporary unblock: %v", err)
	}
	if status := requireDailyStatus(t, service, provider.ID); !status.AutoBlocked {
		t.Fatalf("a later qualifying settlement should re-block: %+v", status)
	}
}

func TestDailyCostLimitFollowsRealBlacklistTransitions(t *testing.T) {
	tests := []struct {
		name             string
		actualMicros     int64
		failureThreshold int
		wantBlocked      bool
	}{
		{name: "failure_below_blacklist_threshold", actualMicros: 950_000, failureThreshold: 2, wantBlocked: false},
		{name: "blacklisted_below_95_percent", actualMicros: 949_999, failureThreshold: 1, wantBlocked: false},
		{name: "blacklisted_at_95_percent", actualMicros: 950_000, failureThreshold: 1, wantBlocked: true},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupBlacklistFixEnv(t)
			setAppSetting(t, "enable_blacklist", "true")
			setAppSetting(t, "blacklist_failure_threshold", fmt.Sprint(test.failureThreshold))
			db, err := xdb.DB("default")
			if err != nil {
				t.Fatalf("get test database: %v", err)
			}
			if err := ensureRequestLogTableWithDB(db); err != nil {
				t.Fatalf("complete request_log schema: %v", err)
			}
			if err := ensureDailyCostLimitTable(); err != nil {
				t.Fatalf("create daily limit table: %v", err)
			}

			provider := Provider{
				ID: int64(20 + index), Name: "observer-" + test.name, Enabled: true,
				DailyCostLimitEnabled: true, DailyCostLimitMicros: microsPerUSD,
			}
			providerService := NewProviderService()
			saveProviderFixture(t, providerService, []Provider{provider})
			appSettings, err := NewAppSettingsService()
			if err != nil {
				t.Fatalf("create app settings service: %v", err)
			}
			settings := appSettings.defaultSettings()
			if _, err := appSettings.SaveAppSettings(settings); err != nil {
				t.Fatalf("save app settings: %v", err)
			}

			blacklist := NewBlacklistService(NewSettingsService(), nil)
			service := NewDailyCostLimitService(providerService, appSettings, NewLogService(providerService))
			service.SetBlacklistService(blacklist)
			blacklist.SetBlacklistObserver(service.OnProviderBlacklisted)
			if err := service.SetActualUsage(CodexPlatform, provider.ID, test.actualMicros); err != nil {
				t.Fatalf("set actual usage: %v", err)
			}
			if err := blacklist.RecordFailure(CodexPlatform, provider.Name); err != nil {
				t.Fatalf("record provider failure: %v", err)
			}
			status := requireDailyStatus(t, service, provider.ID)
			if status.AutoBlocked != test.wantBlocked {
				t.Fatalf("AutoBlocked = %v, want %v: %+v", status.AutoBlocked, test.wantBlocked, status)
			}
		})
	}
}

func TestDailyCostLimitManualActionsAreExplicit(t *testing.T) {
	provider := Provider{
		ID: 3, Name: "manual", Enabled: true,
		DailyCostLimitEnabled: true, DailyCostLimitMicros: 2 * microsPerUSD,
	}
	service, _, _ := setupDailyLimitTestService(
		t,
		provider,
		time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC),
	)

	if err := service.SetActualUsage(CodexPlatform, provider.ID, provider.DailyCostLimitMicros); err != nil {
		t.Fatalf("set over-limit usage: %v", err)
	}
	if err := service.SetActualUsage(CodexPlatform, provider.ID, microsPerUSD); err != nil {
		t.Fatalf("lower actual usage: %v", err)
	}
	if status := requireDailyStatus(t, service, provider.ID); !status.AutoBlocked || status.UsedMicros != microsPerUSD {
		t.Fatalf("lowering usage must not implicitly unblock: %+v", status)
	}

	if err := service.TemporaryUnblock(CodexPlatform, provider.ID); err != nil {
		t.Fatalf("temporary unblock: %v", err)
	}
	if status := requireDailyStatus(t, service, provider.ID); status.Blocked {
		t.Fatalf("temporary unblock should clear today's gates: %+v", status)
	}
	if err := service.ManualBlock(CodexPlatform, provider.ID); err != nil {
		t.Fatalf("manual block: %v", err)
	}
	if status := requireDailyStatus(t, service, provider.ID); !status.ManualBlocked || status.BlockReason != "manual" {
		t.Fatalf("manual block should remain independent: %+v", status)
	}
}

func TestDailyCostLimitDisableClearsGateAndReenableReevaluatesUsage(t *testing.T) {
	provider := Provider{
		ID: 4, Name: "toggle", Enabled: true,
		DailyCostLimitEnabled: true, DailyCostLimitMicros: microsPerUSD,
	}
	service, providerService, _ := setupDailyLimitTestService(
		t,
		provider,
		time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC),
	)
	if err := service.SetActualUsage(CodexPlatform, provider.ID, microsPerUSD); err != nil {
		t.Fatalf("set actual usage: %v", err)
	}

	provider.DailyCostLimitEnabled = false
	saveProviderFixture(t, providerService, []Provider{provider})
	disabled := requireDailyStatus(t, service, provider.ID)
	if disabled.Enabled || disabled.Blocked {
		t.Fatalf("disabling the feature should clear its active gate: %+v", disabled)
	}

	provider.DailyCostLimitEnabled = true
	saveProviderFixture(t, providerService, []Provider{provider})
	if err := service.ManualBlock(CodexPlatform, provider.ID); err != nil {
		t.Fatalf("manual block immediately after re-enabling: %v", err)
	}
	reenabled := requireDailyStatus(t, service, provider.ID)
	if !reenabled.AutoBlocked || !reenabled.ManualBlocked || reenabled.UsedMicros != microsPerUSD {
		t.Fatalf("reenabling should evaluate retained usage: %+v", reenabled)
	}
}

func TestMeetsPercentThresholdRoundsUpWithoutOverflow(t *testing.T) {
	if meetsPercentThreshold(950_000, 1_000_001, 95) {
		t.Fatal("950000 is below 95% of 1000001")
	}
	if !meetsPercentThreshold(950_001, 1_000_001, 95) {
		t.Fatal("950001 should reach the rounded-up 95% threshold")
	}
	if !meetsPercentThreshold(maxMoneyMicros, maxMoneyMicros, 95) {
		t.Fatal("large values should not overflow threshold calculation")
	}
}
