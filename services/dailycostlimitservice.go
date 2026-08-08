package services

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	modelpricing "codeswitch/resources/model-pricing"

	"github.com/daodao97/xgo/xdb"
)

const (
	microsPerUSD         int64 = 1_000_000
	maxMoneyMicros       int64 = 9_000_000_000_000_000
	dailyLimitDateLayout       = "2006-01-02"
)

type dailyCostLimitKey struct {
	Platform   string
	ProviderID ProviderID
	Timezone   string
	Day        string
}

type dailyCostLimitState struct {
	dailyCostLimitKey
	SystemCostMicros       int64
	ManualAdjustmentMicros int64
	AutoBlocked            bool
	ManualBlocked          bool
	FeatureEnabled         bool
	LimitMicros            int64
}

// DailyCostLimitStatus is the current, timezone-scoped state shown in the WebUI.
type DailyCostLimitStatus struct {
	ProviderID             ProviderID `json:"providerId"`
	ProviderName           string     `json:"providerName"`
	Enabled                bool       `json:"enabled"`
	Timezone               string     `json:"timezone"`
	Day                    string     `json:"day"`
	LimitMicros            int64      `json:"limitMicros"`
	SystemCostMicros       int64      `json:"systemCostMicros"`
	ManualAdjustmentMicros int64      `json:"manualAdjustmentMicros"`
	UsedMicros             int64      `json:"usedMicros"`
	UsagePercent           float64    `json:"usagePercent"`
	AutoBlocked            bool       `json:"autoBlocked"`
	ManualBlocked          bool       `json:"manualBlocked"`
	Blocked                bool       `json:"blocked"`
	BlockReason            string     `json:"blockReason"`
}

// DailyCostLimitService keeps the per-Provider daily gate separate from both
// Provider.Enabled and the ordinary timed blacklist.
type DailyCostLimitService struct {
	providerService *ProviderService
	appSettings     *AppSettingsService
	logService      *LogService
	mu              sync.Mutex
	states          map[dailyCostLimitKey]*dailyCostLimitState
	loaded          map[dailyCostLimitKey]bool
	now             func() time.Time
}

func NewDailyCostLimitService(
	providerService *ProviderService,
	appSettings *AppSettingsService,
	logService *LogService,
) *DailyCostLimitService {
	return &DailyCostLimitService{
		providerService: providerService,
		appSettings:     appSettings,
		logService:      logService,
		states:          make(map[dailyCostLimitKey]*dailyCostLimitState),
		loaded:          make(map[dailyCostLimitKey]bool),
		now:             time.Now,
	}
}

func (service *DailyCostLimitService) currentScope() (string, string, time.Time, time.Time, error) {
	timezone := defaultAppTimezone
	if service.appSettings != nil {
		settings, err := service.appSettings.GetAppSettings()
		if err != nil {
			return "", "", time.Time{}, time.Time{}, fmt.Errorf("读取应用时区失败: %w", err)
		}
		timezone = strings.TrimSpace(settings.Timezone)
	}
	if timezone == "" {
		timezone = defaultAppTimezone
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", "", time.Time{}, time.Time{}, fmt.Errorf("加载时区 %q 失败: %w", timezone, err)
	}
	now := service.now().In(location)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	return timezone, now.Format(dailyLimitDateLayout), start, start.AddDate(0, 0, 1), nil
}

func (service *DailyCostLimitService) stateKey(
	platform string,
	providerID ProviderID,
	timezone string,
	day string,
) dailyCostLimitKey {
	return dailyCostLimitKey{
		Platform: platform, ProviderID: providerID, Timezone: timezone, Day: day,
	}
}

func (service *DailyCostLimitService) loadStateLocked(key dailyCostLimitKey) (*dailyCostLimitState, bool, error) {
	if service.loaded[key] {
		state, ok := service.states[key]
		return state, ok, nil
	}
	db, err := xdb.DB("default")
	if err != nil {
		return nil, false, fmt.Errorf("获取数据库连接失败: %w", err)
	}
	var state dailyCostLimitState
	state.dailyCostLimitKey = key
	var autoBlocked, manualBlocked, featureEnabled int
	err = db.QueryRow(`SELECT system_cost_micros, manual_adjustment_micros,
			auto_blocked, manual_blocked, feature_enabled, limit_micros
		FROM provider_daily_cost_limit
		WHERE platform = ? AND provider_id = ? AND timezone = ? AND day_key = ?`,
		key.Platform, key.ProviderID, key.Timezone, key.Day,
	).Scan(
		&state.SystemCostMicros,
		&state.ManualAdjustmentMicros,
		&autoBlocked,
		&manualBlocked,
		&featureEnabled,
		&state.LimitMicros,
	)
	if err == sql.ErrNoRows {
		service.loaded[key] = true
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("读取每日费用限额状态失败: %w", err)
	}
	service.loaded[key] = true
	state.AutoBlocked = autoBlocked != 0
	state.ManualBlocked = manualBlocked != 0
	state.FeatureEnabled = featureEnabled != 0
	service.states[key] = &state
	return &state, true, nil
}

func (service *DailyCostLimitService) ensureStateLocked(
	provider Provider,
	key dailyCostLimitKey,
	start time.Time,
	end time.Time,
) (*dailyCostLimitState, error) {
	if state, exists, err := service.loadStateLocked(key); err != nil || exists {
		return state, err
	}
	systemCost, err := service.calculateSystemCost(provider, start, end)
	if err != nil {
		return nil, err
	}
	state := &dailyCostLimitState{
		dailyCostLimitKey: key,
		SystemCostMicros:  systemCost,
		FeatureEnabled:    provider.DailyCostLimitEnabled,
		LimitMicros:       provider.DailyCostLimitMicros,
	}
	if provider.DailyCostLimitEnabled && provider.DailyCostLimitMicros > 0 {
		state.AutoBlocked = state.usedMicros() >= provider.DailyCostLimitMicros
	}
	if err := service.writeStateLocked(state); err != nil {
		return nil, err
	}
	service.states[key] = state
	return state, nil
}

func (service *DailyCostLimitService) calculateSystemCost(
	provider Provider,
	start time.Time,
	end time.Time,
) (int64, error) {
	if service.logService == nil || service.logService.pricing == nil {
		return 0, nil
	}
	db, err := xdb.DB("default")
	if err != nil {
		return 0, fmt.Errorf("获取数据库连接失败: %w", err)
	}
	rows, err := db.Query(`SELECT COALESCE(model, ''), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
			COALESCE(reasoning_tokens, 0), COALESCE(cache_read_tokens, 0), COALESCE(service_tier, '')
		FROM request_log
		WHERE platform = ? AND provider = ? AND created_at >= ? AND created_at < ?`,
		CodexPlatform,
		provider.Name,
		start.UTC().Format(timeLayout),
		end.UTC().Format(timeLayout),
	)
	if err != nil {
		if isNoSuchTableErr(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("读取当日请求费用失败: %w", err)
	}
	defer rows.Close()

	var total int64
	for rows.Next() {
		var model, serviceTier string
		var inputTokens, outputTokens, reasoningTokens, cacheReadTokens int
		if err := rows.Scan(
			&model,
			&inputTokens,
			&outputTokens,
			&reasoningTokens,
			&cacheReadTokens,
			&serviceTier,
		); err != nil {
			return 0, fmt.Errorf("读取当日请求费用行失败: %w", err)
		}
		usage := modelpricing.UsageSnapshot{
			InputTokens:     inputTokens,
			OutputTokens:    outputTokens,
			ReasoningTokens: reasoningTokens,
			CacheReadTokens: cacheReadTokens,
			ServiceTier:     modelpricing.ServiceTier(strings.ToLower(strings.TrimSpace(serviceTier))),
		}
		cost := service.logService.pricing.CalculateCost(model, usage)
		cost = applyCostMultiplier(cost, provider.EffectiveCostMultiplier())
		total = addMoneyMicros(total, costToMicros(cost.TotalCost))
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("遍历当日请求费用失败: %w", err)
	}
	return total, nil
}

func (service *DailyCostLimitService) writeStateLocked(state *dailyCostLimitState) error {
	const query = `INSERT INTO provider_daily_cost_limit (
			platform, provider_id, timezone, day_key,
			system_cost_micros, manual_adjustment_micros,
			auto_blocked, manual_blocked, feature_enabled, limit_micros, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(platform, provider_id, timezone, day_key) DO UPDATE SET
			system_cost_micros = excluded.system_cost_micros,
			manual_adjustment_micros = excluded.manual_adjustment_micros,
			auto_blocked = excluded.auto_blocked,
			manual_blocked = excluded.manual_blocked,
			feature_enabled = excluded.feature_enabled,
			limit_micros = excluded.limit_micros,
			updated_at = CURRENT_TIMESTAMP`
	args := []interface{}{
		state.Platform,
		state.ProviderID,
		state.Timezone,
		state.Day,
		state.SystemCostMicros,
		state.ManualAdjustmentMicros,
		boolToInt(state.AutoBlocked),
		boolToInt(state.ManualBlocked),
		boolToInt(state.FeatureEnabled),
		state.LimitMicros,
	}
	if GlobalDBQueue != nil {
		if err := GlobalDBQueue.Exec(query, args...); err != nil {
			return fmt.Errorf("保存每日费用限额状态失败: %w", err)
		}
		return nil
	}
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}
	if _, err := db.Exec(query, args...); err != nil {
		return fmt.Errorf("保存每日费用限额状态失败: %w", err)
	}
	return nil
}

func (state *dailyCostLimitState) usedMicros() int64 {
	systemCost := state.SystemCostMicros
	if systemCost < 0 {
		systemCost = 0
	} else if systemCost > maxMoneyMicros {
		systemCost = maxMoneyMicros
	}
	if state.ManualAdjustmentMicros > 0 && systemCost > maxMoneyMicros-state.ManualAdjustmentMicros {
		return maxMoneyMicros
	}
	if state.ManualAdjustmentMicros < 0 && state.ManualAdjustmentMicros <= -systemCost {
		return 0
	}
	used := systemCost + state.ManualAdjustmentMicros
	if used < 0 {
		return 0
	}
	if used > maxMoneyMicros {
		return maxMoneyMicros
	}
	return used
}

func (service *DailyCostLimitService) syncConfigurationLocked(
	state *dailyCostLimitState,
	provider Provider,
) error {
	next := *state
	wasEnabled := next.FeatureEnabled
	limitChanged := next.LimitMicros != provider.DailyCostLimitMicros
	next.FeatureEnabled = provider.DailyCostLimitEnabled
	next.LimitMicros = provider.DailyCostLimitMicros

	if !provider.DailyCostLimitEnabled {
		next.AutoBlocked = false
		next.ManualBlocked = false
	} else if !wasEnabled {
		// Turning the feature back on starts from today's retained usage, but a
		// previous manual block is not restored.
		used := next.usedMicros()
		next.AutoBlocked = next.LimitMicros > 0 && used >= next.LimitMicros
		next.ManualBlocked = false
	} else if limitChanged && next.LimitMicros > 0 {
		used := next.usedMicros()
		if used >= next.LimitMicros {
			next.AutoBlocked = true
		}
	}

	if next == *state {
		return nil
	}
	if err := service.writeStateLocked(&next); err != nil {
		return err
	}
	*state = next
	return nil
}

func (service *DailyCostLimitService) IsProviderBlocked(platform string, provider Provider) (bool, error) {
	if err := requireCodexPlatform(platform); err != nil {
		return false, err
	}
	platform = CodexPlatform
	timezone, day, start, end, err := service.currentScope()
	if err != nil {
		return provider.DailyCostLimitEnabled, err
	}
	key := service.stateKey(platform, provider.ID, timezone, day)

	service.mu.Lock()
	defer service.mu.Unlock()
	if !provider.DailyCostLimitEnabled {
		state, exists, err := service.loadStateLocked(key)
		if err != nil || !exists {
			return false, err
		}
		if err := service.syncConfigurationLocked(state, provider); err != nil {
			return false, err
		}
		return false, nil
	}
	state, err := service.ensureStateLocked(provider, key, start, end)
	if err != nil {
		return true, err
	}
	if err := service.syncConfigurationLocked(state, provider); err != nil {
		return true, err
	}
	return state.AutoBlocked || state.ManualBlocked, nil
}

// SettleRequest synchronously records locally priced usage before the request
// log is queued. Already in-flight requests are allowed to finish, while later
// routing decisions see the updated daily state immediately.
func (service *DailyCostLimitService) SettleRequest(provider Provider, requestLog *RequestLog) error {
	if requestLog == nil || requestLog.Platform != CodexPlatform || service.logService == nil ||
		service.logService.pricing == nil {
		return nil
	}
	usage := modelpricing.UsageSnapshot{
		InputTokens:     requestLog.InputTokens,
		OutputTokens:    requestLog.OutputTokens,
		ReasoningTokens: requestLog.ReasoningTokens,
		CacheReadTokens: requestLog.CacheReadTokens,
		ServiceTier:     modelpricing.ServiceTier(strings.ToLower(strings.TrimSpace(requestLog.ServiceTier))),
	}
	cost := service.logService.pricing.CalculateCost(requestLog.Model, usage)
	cost = applyCostMultiplier(cost, provider.EffectiveCostMultiplier())
	costMicros := costToMicros(cost.TotalCost)
	if costMicros <= 0 {
		return nil
	}

	timezone, day, start, end, err := service.currentScope()
	if err != nil {
		return err
	}
	key := service.stateKey(CodexPlatform, provider.ID, timezone, day)
	service.mu.Lock()
	defer service.mu.Unlock()

	var state *dailyCostLimitState
	if provider.DailyCostLimitEnabled {
		state, err = service.ensureStateLocked(provider, key, start, end)
	} else {
		var exists bool
		state, exists, err = service.loadStateLocked(key)
		if err == nil && !exists {
			return nil
		}
	}
	if err != nil {
		return err
	}
	if err := service.syncConfigurationLocked(state, provider); err != nil {
		return err
	}

	next := *state
	next.SystemCostMicros = addMoneyMicros(next.SystemCostMicros, costMicros)
	if provider.DailyCostLimitEnabled {
		used := next.usedMicros()
		if next.LimitMicros > 0 && used >= next.LimitMicros {
			next.AutoBlocked = true
		}
	}
	writeErr := service.writeStateLocked(&next)
	// The in-process gate must still see completed usage when persistence has a
	// transient failure. A later successful settlement persists the aggregate.
	*state = next
	return writeErr
}

func (service *DailyCostLimitService) GetStatuses(platform string) ([]DailyCostLimitStatus, error) {
	if err := requireCodexPlatform(platform); err != nil {
		return nil, err
	}
	providers, err := service.providerService.LoadProviders(CodexPlatform)
	if err != nil {
		return nil, fmt.Errorf("加载 Provider 失败: %w", err)
	}
	statuses := make([]DailyCostLimitStatus, 0, len(providers))
	for _, provider := range providers {
		status, err := service.statusForProvider(provider)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (service *DailyCostLimitService) statusForProvider(provider Provider) (DailyCostLimitStatus, error) {
	timezone, day, start, end, err := service.currentScope()
	if err != nil {
		return DailyCostLimitStatus{}, err
	}
	status := DailyCostLimitStatus{
		ProviderID: provider.ID, ProviderName: provider.Name, Enabled: provider.DailyCostLimitEnabled,
		Timezone: timezone, Day: day, LimitMicros: provider.DailyCostLimitMicros,
	}
	key := service.stateKey(CodexPlatform, provider.ID, timezone, day)
	service.mu.Lock()
	defer service.mu.Unlock()

	var state *dailyCostLimitState
	if provider.DailyCostLimitEnabled {
		state, err = service.ensureStateLocked(provider, key, start, end)
	} else {
		var exists bool
		state, exists, err = service.loadStateLocked(key)
		if err == nil && !exists {
			return status, nil
		}
	}
	if err != nil {
		return DailyCostLimitStatus{}, err
	}
	if err := service.syncConfigurationLocked(state, provider); err != nil {
		return DailyCostLimitStatus{}, err
	}
	return buildDailyCostLimitStatus(provider, state), nil
}

func buildDailyCostLimitStatus(provider Provider, state *dailyCostLimitState) DailyCostLimitStatus {
	used := state.usedMicros()
	percent := 0.0
	if provider.DailyCostLimitMicros > 0 {
		percent = float64(used) / float64(provider.DailyCostLimitMicros) * 100
	}
	reason := ""
	switch {
	case state.AutoBlocked && state.ManualBlocked:
		reason = "quota_and_manual"
	case state.AutoBlocked:
		reason = "quota"
	case state.ManualBlocked:
		reason = "manual"
	}
	return DailyCostLimitStatus{
		ProviderID:             provider.ID,
		ProviderName:           provider.Name,
		Enabled:                provider.DailyCostLimitEnabled,
		Timezone:               state.Timezone,
		Day:                    state.Day,
		LimitMicros:            provider.DailyCostLimitMicros,
		SystemCostMicros:       state.SystemCostMicros,
		ManualAdjustmentMicros: state.ManualAdjustmentMicros,
		UsedMicros:             used,
		UsagePercent:           percent,
		AutoBlocked:            state.AutoBlocked,
		ManualBlocked:          state.ManualBlocked,
		Blocked:                state.AutoBlocked || state.ManualBlocked,
		BlockReason:            reason,
	}
}

func (service *DailyCostLimitService) SetActualUsage(platform string, providerID ProviderID, actualMicros int64) error {
	if actualMicros < 0 || actualMicros > maxMoneyMicros {
		return fmt.Errorf("今日实际用量必须在 0-%d 微美元之间", maxMoneyMicros)
	}
	provider, err := service.findProvider(platform, providerID, "")
	if err != nil {
		return err
	}
	if !provider.DailyCostLimitEnabled {
		return fmt.Errorf("Provider %s 未启用每日费用限额", provider.Name)
	}
	timezone, day, start, end, err := service.currentScope()
	if err != nil {
		return err
	}
	key := service.stateKey(CodexPlatform, provider.ID, timezone, day)
	service.mu.Lock()
	defer service.mu.Unlock()
	state, err := service.ensureStateLocked(provider, key, start, end)
	if err != nil {
		return err
	}
	if err := service.syncConfigurationLocked(state, provider); err != nil {
		return err
	}
	next := *state
	next.ManualAdjustmentMicros = actualMicros - next.SystemCostMicros
	if actualMicros >= provider.DailyCostLimitMicros {
		next.AutoBlocked = true
	}
	if err := service.writeStateLocked(&next); err != nil {
		return err
	}
	*state = next
	return nil
}

func (service *DailyCostLimitService) ManualBlock(platform string, providerID ProviderID) error {
	return service.setBlockState(platform, providerID, true)
}

// TemporaryUnblock clears today's daily-limit gates only. It does not create an
// exemption: a later 100% settlement can block the Provider again.
func (service *DailyCostLimitService) TemporaryUnblock(platform string, providerID ProviderID) error {
	return service.setBlockState(platform, providerID, false)
}

func (service *DailyCostLimitService) setBlockState(platform string, providerID ProviderID, blocked bool) error {
	provider, err := service.findProvider(platform, providerID, "")
	if err != nil {
		return err
	}
	if !provider.DailyCostLimitEnabled {
		return fmt.Errorf("Provider %s 未启用每日费用限额", provider.Name)
	}
	timezone, day, start, end, err := service.currentScope()
	if err != nil {
		return err
	}
	key := service.stateKey(CodexPlatform, provider.ID, timezone, day)
	service.mu.Lock()
	defer service.mu.Unlock()
	state, err := service.ensureStateLocked(provider, key, start, end)
	if err != nil {
		return err
	}
	if err := service.syncConfigurationLocked(state, provider); err != nil {
		return err
	}
	next := *state
	if blocked {
		next.ManualBlocked = true
	} else {
		next.ManualBlocked = false
		next.AutoBlocked = false
	}
	if err := service.writeStateLocked(&next); err != nil {
		return err
	}
	*state = next
	return nil
}

func (service *DailyCostLimitService) findProvider(platform string, providerID ProviderID, providerName string) (Provider, error) {
	if err := requireCodexPlatform(platform); err != nil {
		return Provider{}, err
	}
	providers, err := service.providerService.LoadProviders(CodexPlatform)
	if err != nil {
		return Provider{}, fmt.Errorf("加载 Provider 失败: %w", err)
	}
	canonicalName := ResolveProviderAlias(CodexPlatform, strings.TrimSpace(providerName))
	for _, provider := range providers {
		if !providerID.IsZero() && provider.ID == providerID {
			return provider, nil
		}
		if providerID.IsZero() && provider.Name == canonicalName {
			return provider, nil
		}
	}
	if !providerID.IsZero() {
		return Provider{}, fmt.Errorf("未找到 Provider ID: %s", providerID)
	}
	return Provider{}, fmt.Errorf("未找到 Provider: %s", providerName)
}

func meetsPercentThreshold(usedMicros int64, limitMicros int64, percent int64) bool {
	if usedMicros < 0 || limitMicros <= 0 || percent <= 0 {
		return false
	}
	// Divide before multiplying so manually edited/corrupt configuration cannot
	// overflow even if it bypassed Provider validation.
	threshold := (limitMicros/100)*percent + (limitMicros%100*percent+99)/100
	return usedMicros >= threshold
}

func costToMicros(cost float64) int64 {
	if math.IsNaN(cost) || math.IsInf(cost, 0) || cost <= 0 {
		return 0
	}
	if cost >= float64(maxMoneyMicros)/float64(microsPerUSD) {
		return maxMoneyMicros
	}
	return int64(math.Round(cost * float64(microsPerUSD)))
}

func addMoneyMicros(current int64, delta int64) int64 {
	if delta <= 0 {
		return current
	}
	if current >= maxMoneyMicros-delta {
		return maxMoneyMicros
	}
	return current + delta
}
