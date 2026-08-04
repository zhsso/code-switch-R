package services

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	modelpricing "codeswitch/resources/model-pricing"

	"github.com/daodao97/xgo/xdb"
)

const timeLayout = "2006-01-02 15:04:05"

type LogService struct {
	pricing         *modelpricing.Service
	providerService *ProviderService
}

type costContext struct {
	multipliers    map[string]float64
	resolveAliases bool
}

func (ls *LogService) CostSince(start string, platform string) (float64, error) {
	if platform != "" {
		if err := requireCodexPlatform(platform); err != nil {
			return 0, err
		}
	}
	startTime, err := parseTimeInput(start)
	if err != nil {
		return 0, err
	}
	costs, err := ls.newCostContext()
	if err != nil {
		return 0, err
	}
	model := xdb.New("request_log")
	// created_at 由 DEFAULT CURRENT_TIMESTAMP 落库,存的是 UTC 文本,
	// 查询边界必须先转 UTC 再格式化,否则非 UTC 时区下与存储口径错位
	options := []xdb.Option{
		xdb.WhereGte("created_at", startTime.UTC().Format(timeLayout)),
		xdb.WhereEq("platform", CodexPlatform),
		xdb.Field(
			"provider",
			"model",
			"input_tokens",
			"output_tokens",
			"reasoning_tokens",
			"cache_read_tokens",
			"service_tier",
		),
	}
	records, err := model.Selects(options...)
	if err != nil {
		if errors.Is(err, xdb.ErrNotFound) || isNoSuchTableErr(err) {
			return 0, nil
		}
		return 0, err
	}
	total := 0.0
	for _, record := range records {
		usage := buildSnapshotFromRecord(record)
		cost := ls.calculateCost(costs, record.GetString("provider"), record.GetString("model"), usage)
		total += cost.TotalCost
	}
	return total, nil
}

// buildSnapshotFromRecord 从 Codex request_log 记录构造定价输入。
func buildSnapshotFromRecord(record xdb.Record) modelpricing.UsageSnapshot {
	return modelpricing.UsageSnapshot{
		InputTokens:     record.GetInt("input_tokens"),
		OutputTokens:    record.GetInt("output_tokens"),
		ReasoningTokens: record.GetInt("reasoning_tokens"),
		CacheReadTokens: record.GetInt("cache_read_tokens"),
		ServiceTier:     modelpricing.ServiceTier(strings.ToLower(strings.TrimSpace(record.GetString("service_tier")))),
	}
}

func NewLogService(providerServices ...*ProviderService) *LogService {
	svc, err := modelpricing.DefaultService()
	if err != nil {
		log.Printf("pricing service init failed: %v", err)
	}
	var providerService *ProviderService
	if len(providerServices) > 0 {
		providerService = providerServices[0]
	}
	return &LogService{pricing: svc, providerService: providerService}
}

func (ls *LogService) newCostContext() (*costContext, error) {
	context := &costContext{multipliers: make(map[string]float64)}
	if ls == nil || ls.providerService == nil {
		return context, nil
	}

	providers, err := ls.providerService.LoadProviders(CodexPlatform)
	if err != nil {
		return nil, fmt.Errorf("加载 Provider 费用倍率失败: %w", err)
	}
	for _, provider := range providers {
		if err := validateCostMultiplier(provider.CostMultiplier); err != nil {
			return nil, fmt.Errorf("Provider %q: %w", provider.Name, err)
		}
		name := strings.TrimSpace(provider.Name)
		if name != "" {
			context.multipliers[name] = provider.EffectiveCostMultiplier()
		}
	}
	context.resolveAliases = true
	return context, nil
}

func (context *costContext) multiplierFor(provider string) float64 {
	if context == nil {
		return 1
	}
	name := strings.TrimSpace(provider)
	if multiplier, ok := context.multipliers[name]; ok {
		return multiplier
	}
	if context.resolveAliases && name != "" {
		canonical := ResolveProviderAlias(CodexPlatform, name)
		if multiplier, ok := context.multipliers[canonical]; ok {
			context.multipliers[name] = multiplier
			return multiplier
		}
	}
	context.multipliers[name] = 1
	return 1
}

// CleanupOldRecords removes request and cost history older than the configured
// retention window. Capture session metadata is removed only after its session
// is closed and no request rows remain.
func (ls *LogService) CleanupOldRecords(daysToKeep int) (int64, error) {
	if daysToKeep < 1 {
		return 0, fmt.Errorf("history retention must be at least one day")
	}
	db, err := xdb.DB("default")
	if err != nil {
		return 0, fmt.Errorf("get database: %w", err)
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -daysToKeep).Format(timeLayout)
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin history cleanup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.Exec(
		`DELETE FROM request_log WHERE platform = ? AND created_at < ?`,
		CodexPlatform,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("delete request history: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM capture_session
		WHERE (ended_at IS NOT NULL OR interrupted != 0)
		AND NOT EXISTS (
			SELECT 1 FROM request_log
			WHERE request_log.capture_session_id = capture_session.id
			AND request_log.capture_session_id != 0
		)`); err != nil {
		return 0, fmt.Errorf("delete empty capture sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit history cleanup: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read cleanup result: %w", err)
	}
	return rows, nil
}

func (ls *LogService) ListRequestLogs(platform string, provider string, limit int) ([]ReqeustLog, error) {
	if platform != "" {
		if err := requireCodexPlatform(platform); err != nil {
			return nil, err
		}
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	costs, err := ls.newCostContext()
	if err != nil {
		return nil, err
	}
	model := xdb.New("request_log")
	options := []xdb.Option{
		// 显式投影：绝不把抓包大字段（request/response body 等）拉进列表——
		// 一页 200 行最坏能带出几百 MB 正文。has_capture 供前端判断可否展开详情。
		// 谓词须与 capturesession.go 的 captureRowPredicate 覆盖同一批列
		xdb.Field("id, platform, model, provider, http_code, input_tokens, output_tokens, " +
			"cache_read_tokens, reasoning_tokens, is_stream, duration_sec, created_at, service_tier, " +
			"(request_url != '' OR request_headers != '' OR request_body != '' OR response_headers != '' OR response_body != '' OR body_truncated != 0 OR body_bytes != 0 OR response_truncated != 0 OR response_bytes != 0 OR budget_skipped != 0) AS has_capture"),
		xdb.WhereEq("platform", CodexPlatform),
		xdb.OrderByDesc("id"),
		xdb.Limit(limit),
	}
	if provider != "" {
		options = append(options, xdb.WhereEq("provider", provider))
	}
	records, err := model.Selects(options...)
	if err != nil {
		return nil, err
	}
	logs := make([]ReqeustLog, 0, len(records))
	for _, record := range records {
		logEntry := ReqeustLog{
			ID:              record.GetInt64("id"),
			Platform:        record.GetString("platform"),
			Model:           record.GetString("model"),
			Provider:        record.GetString("provider"),
			HttpCode:        record.GetInt("http_code"),
			InputTokens:     record.GetInt("input_tokens"),
			OutputTokens:    record.GetInt("output_tokens"),
			CacheReadTokens: record.GetInt("cache_read_tokens"),
			ReasoningTokens: record.GetInt("reasoning_tokens"),
			CreatedAt:       record.GetString("created_at"),
			IsStream:        record.GetBool("is_stream"),
			DurationSec:     record.GetFloat64("duration_sec"),
			ServiceTier:     record.GetString("service_tier"),
			HasCapture:      record.GetBool("has_capture"),
		}
		ls.decorateCost(&logEntry, costs)
		logs = append(logs, logEntry)
	}
	return logs, nil
}

// RequestLogDetail 单条日志的抓包详情（按需读取，避免列表携带大字段）。
// 每个大字段仅返回有界预览（capturePreview），避免大正文阻塞浏览器。
type RequestLogDetail struct {
	ID         int64  `json:"id"`
	Platform   string `json:"platform"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	CreatedAt  string `json:"created_at"`
	RequestURL string `json:"request_url"`
	// 请求
	RequestHeaders     string `json:"request_headers"`
	RequestBody        string `json:"request_body"`
	RequestBodyPreview bool   `json:"request_body_preview"` // 正文被预览截断
	BodyTruncated      bool   `json:"body_truncated"`       // 采集时即超 captureFieldLimit
	BodyBytes          int    `json:"body_bytes"`
	// 响应
	ResponseHeaders     string `json:"response_headers"`
	ResponseBody        string `json:"response_body"`
	ResponseBodyPreview bool   `json:"response_body_preview"`
	RespTruncated       bool   `json:"response_truncated"`
	RespBytes           int    `json:"response_bytes"`
	BudgetSkipped       bool   `json:"budget_skipped"`
}

// GetRequestLogDetail 读取单条日志的抓包详情（全量不脱敏录制的内容）。
// 大字段返回有界预览
func (ls *LogService) GetRequestLogDetail(id int64) (*RequestLogDetail, error) {
	model := xdb.New("request_log")
	records, err := model.Selects(
		xdb.Field("id, platform, provider, model, created_at, request_url, request_headers, request_body, body_truncated, body_bytes, response_headers, response_body, response_truncated, response_bytes, budget_skipped"),
		xdb.WhereEq("id", id),
		xdb.WhereEq("platform", CodexPlatform),
		xdb.Limit(1),
	)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("未找到 ID 为 %d 的日志", id)
	}
	record := records[0]
	reqBody, reqPreview := capturePreview(record.GetString("request_body"))
	respBody, respPreview := capturePreview(record.GetString("response_body"))
	return &RequestLogDetail{
		ID:                  record.GetInt64("id"),
		Platform:            record.GetString("platform"),
		Provider:            record.GetString("provider"),
		Model:               record.GetString("model"),
		CreatedAt:           record.GetString("created_at"),
		RequestURL:          record.GetString("request_url"),
		RequestHeaders:      record.GetString("request_headers"),
		RequestBody:         reqBody,
		RequestBodyPreview:  reqPreview,
		BodyTruncated:       record.GetBool("body_truncated"),
		BodyBytes:           record.GetInt("body_bytes"),
		ResponseHeaders:     record.GetString("response_headers"),
		ResponseBody:        respBody,
		ResponseBodyPreview: respPreview,
		RespTruncated:       record.GetBool("response_truncated"),
		RespBytes:           record.GetInt("response_bytes"),
		BudgetSkipped:       record.GetBool("budget_skipped"),
	}, nil
}

func (ls *LogService) ListProviders(platform string) ([]string, error) {
	if platform != "" {
		if err := requireCodexPlatform(platform); err != nil {
			return nil, err
		}
	}
	model := xdb.New("request_log")
	options := []xdb.Option{
		xdb.Field("DISTINCT provider as provider"),
		xdb.WhereNotEq("provider", ""),
		xdb.WhereEq("platform", CodexPlatform),
		xdb.OrderByAsc("provider"),
	}
	records, err := model.Selects(options...)
	if err != nil {
		return nil, err
	}
	providers := make([]string, 0, len(records))
	for _, record := range records {
		name := strings.TrimSpace(record.GetString("provider"))
		if name != "" {
			providers = append(providers, name)
		}
	}
	return providers, nil
}

func (ls *LogService) HeatmapStats(days int) ([]HeatmapStat, error) {
	if days <= 0 {
		days = 30
	}
	totalHours := days * 24
	if totalHours <= 0 {
		totalHours = 24
	}
	rangeStart := startOfHour(time.Now())
	if totalHours > 1 {
		rangeStart = rangeStart.Add(-time.Duration(totalHours-1) * time.Hour)
	}
	costs, err := ls.newCostContext()
	if err != nil {
		return nil, err
	}
	model := xdb.New("request_log")
	// created_at 存的是 UTC 文本,预滤边界须转 UTC,否则东部时区丢失窗口最旧的
	// 时区偏移小时数、西部时区多取窗口外旧记录
	options := []xdb.Option{
		xdb.WhereGe("created_at", rangeStart.UTC().Format(timeLayout)),
		xdb.WhereEq("platform", CodexPlatform),
		xdb.Field(
			"provider",
			"model",
			"input_tokens",
			"output_tokens",
			"reasoning_tokens",
			"cache_read_tokens",
			"service_tier",
			"created_at",
		),
		xdb.OrderByDesc("created_at"),
	}
	records, err := model.Selects(options...)
	if err != nil {
		if errors.Is(err, xdb.ErrNotFound) || isNoSuchTableErr(err) {
			return []HeatmapStat{}, nil
		}
		return nil, err
	}
	hourBuckets := map[int64]*HeatmapStat{}
	for _, record := range records {
		createdAt, _ := parseCreatedAt(record)
		if createdAt.IsZero() {
			continue
		}
		// 兜底过滤:非标准格式的 created_at 可能穿过 SQL 预滤,窗口外记录直接跳过
		if createdAt.Before(rangeStart) {
			continue
		}
		hourStart := startOfHour(createdAt)
		hourKey := hourStart.Unix()
		bucket := hourBuckets[hourKey]
		if bucket == nil {
			bucket = &HeatmapStat{Day: hourStart.Format("01-02 15")}
			hourBuckets[hourKey] = bucket
		}
		bucket.TotalRequests++
		usage := buildSnapshotFromRecord(record)
		bucket.InputTokens += int64(usage.InputTokens)
		bucket.OutputTokens += int64(usage.OutputTokens)
		bucket.ReasoningTokens += int64(usage.ReasoningTokens)
		cost := ls.calculateCost(costs, record.GetString("provider"), record.GetString("model"), usage)
		bucket.TotalCost += cost.TotalCost
	}
	if len(hourBuckets) == 0 {
		return []HeatmapStat{}, nil
	}
	hourKeys := make([]int64, 0, len(hourBuckets))
	for key := range hourBuckets {
		hourKeys = append(hourKeys, key)
	}
	sort.Slice(hourKeys, func(i, j int) bool {
		return hourKeys[i] < hourKeys[j]
	})
	stats := make([]HeatmapStat, 0, min(len(hourKeys), totalHours))
	for i := len(hourKeys) - 1; i >= 0 && len(stats) < totalHours; i-- {
		stats = append(stats, *hourBuckets[hourKeys[i]])
	}
	return stats, nil
}

func (ls *LogService) StatsSince(platform string) (LogStats, error) {
	if platform != "" {
		if err := requireCodexPlatform(platform); err != nil {
			return LogStats{}, err
		}
	}
	const seriesHours = 24

	stats := LogStats{
		Series: make([]LogStatsSeries, 0, seriesHours),
	}
	costs, err := ls.newCostContext()
	if err != nil {
		return stats, err
	}
	now := time.Now()
	model := xdb.New("request_log")
	seriesStart := startOfDay(now)
	seriesEnd := seriesStart.Add(seriesHours * time.Hour)
	queryStart := seriesStart.Add(-24 * time.Hour)
	summaryStart := seriesStart
	options := []xdb.Option{
		xdb.WhereGte("created_at", queryStart.Format(timeLayout)),
		xdb.WhereEq("platform", CodexPlatform),
		xdb.Field(
			"provider",
			"model",
			"input_tokens",
			"output_tokens",
			"reasoning_tokens",
			"cache_read_tokens",
			"service_tier",
			"created_at",
		),
		xdb.OrderByAsc("created_at"),
	}
	records, err := model.Selects(options...)
	if err != nil {
		if errors.Is(err, xdb.ErrNotFound) || isNoSuchTableErr(err) {
			return stats, nil
		}
		return stats, err
	}

	seriesBuckets := make([]*LogStatsSeries, seriesHours)
	for i := 0; i < seriesHours; i++ {
		bucketTime := seriesStart.Add(time.Duration(i) * time.Hour)
		seriesBuckets[i] = &LogStatsSeries{
			Day: bucketTime.Format(timeLayout),
		}
	}

	for _, record := range records {
		createdAt, hasTime := parseCreatedAt(record)
		dayKey := dayFromTimestamp(record.GetString("created_at"))
		isToday := dayKey == seriesStart.Format("2006-01-02")

		if hasTime {
			if createdAt.Before(seriesStart) || !createdAt.Before(seriesEnd) {
				continue
			}
		} else {
			if !isToday {
				continue
			}
			createdAt = seriesStart
		}

		bucketIndex := 0
		if hasTime {
			bucketIndex = int(createdAt.Sub(seriesStart) / time.Hour)
			if bucketIndex < 0 {
				bucketIndex = 0
			}
			if bucketIndex >= seriesHours {
				bucketIndex = seriesHours - 1
			}
		}
		bucket := seriesBuckets[bucketIndex]
		usage := buildSnapshotFromRecord(record)
		cost := ls.calculateCost(costs, record.GetString("provider"), record.GetString("model"), usage)

		bucket.TotalRequests++
		bucket.InputTokens += int64(usage.InputTokens)
		bucket.OutputTokens += int64(usage.OutputTokens)
		bucket.ReasoningTokens += int64(usage.ReasoningTokens)
		bucket.CacheReadTokens += int64(usage.CacheReadTokens)
		bucket.TotalCost += cost.TotalCost

		if createdAt.IsZero() || createdAt.Before(summaryStart) {
			continue
		}
		stats.TotalRequests++
		stats.InputTokens += int64(usage.InputTokens)
		stats.OutputTokens += int64(usage.OutputTokens)
		stats.ReasoningTokens += int64(usage.ReasoningTokens)
		stats.CacheReadTokens += int64(usage.CacheReadTokens)
		stats.CostInput += cost.InputCost
		stats.CostOutput += cost.OutputCost
		stats.CostCacheRead += cost.CacheReadCost
		stats.CostTotal += cost.TotalCost
	}

	for i := 0; i < seriesHours; i++ {
		if bucket := seriesBuckets[i]; bucket != nil {
			stats.Series = append(stats.Series, *bucket)
		} else {
			bucketTime := seriesStart.Add(time.Duration(i) * time.Hour)
			stats.Series = append(stats.Series, LogStatsSeries{
				Day: bucketTime.Format(timeLayout),
			})
		}
	}

	return stats, nil
}

func (ls *LogService) ProviderDailyStats(platform string) ([]ProviderDailyStat, error) {
	if platform != "" {
		if err := requireCodexPlatform(platform); err != nil {
			return nil, err
		}
	}
	costs, err := ls.newCostContext()
	if err != nil {
		return nil, err
	}
	start := startOfDay(time.Now())
	end := start.Add(24 * time.Hour)
	queryStart := start.Add(-24 * time.Hour)
	model := xdb.New("request_log")
	options := []xdb.Option{
		xdb.WhereGte("created_at", queryStart.Format(timeLayout)),
		xdb.WhereEq("platform", CodexPlatform),
		xdb.Field(
			"provider",
			"model",
			"http_code",
			"input_tokens",
			"output_tokens",
			"reasoning_tokens",
			"cache_read_tokens",
			"service_tier",
			"created_at",
		),
	}
	records, err := model.Selects(options...)
	if err != nil {
		if errors.Is(err, xdb.ErrNotFound) || isNoSuchTableErr(err) {
			return []ProviderDailyStat{}, nil
		}
		return nil, err
	}
	statMap := map[string]*ProviderDailyStat{}
	for _, record := range records {
		provider := strings.TrimSpace(record.GetString("provider"))
		if provider == "" {
			provider = "(unknown)"
		}
		createdAt, hasTime := parseCreatedAt(record)
		if hasTime {
			if createdAt.Before(start) || !createdAt.Before(end) {
				continue
			}
		} else {
			dayKey := dayFromTimestamp(record.GetString("created_at"))
			if dayKey != start.Format("2006-01-02") {
				continue
			}
		}
		stat := statMap[provider]
		if stat == nil {
			stat = &ProviderDailyStat{Provider: provider}
			statMap[provider] = stat
		}
		httpCode := record.GetInt("http_code")
		usage := buildSnapshotFromRecord(record)
		cost := ls.calculateCost(costs, provider, record.GetString("model"), usage)
		stat.TotalRequests++
		// 只有 HTTP 200-299 才算成功，其他（包括 0）都算失败
		if httpCode >= 200 && httpCode < 300 {
			stat.SuccessfulRequests++
		} else {
			stat.FailedRequests++
		}
		stat.InputTokens += int64(usage.InputTokens)
		stat.OutputTokens += int64(usage.OutputTokens)
		stat.ReasoningTokens += int64(usage.ReasoningTokens)
		stat.CacheReadTokens += int64(usage.CacheReadTokens)
		stat.CostTotal += cost.TotalCost
	}
	stats := make([]ProviderDailyStat, 0, len(statMap))
	for _, stat := range statMap {
		if stat.TotalRequests > 0 {
			stat.SuccessRate = float64(stat.SuccessfulRequests) / float64(stat.TotalRequests)
		}
		stats = append(stats, *stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].TotalRequests == stats[j].TotalRequests {
			return stats[i].Provider < stats[j].Provider
		}
		return stats[i].TotalRequests > stats[j].TotalRequests
	})
	return stats, nil
}

func (ls *LogService) decorateCost(logEntry *ReqeustLog, costs *costContext) {
	if ls == nil || ls.pricing == nil || logEntry == nil {
		return
	}
	usage := modelpricing.UsageSnapshot{
		InputTokens:     logEntry.InputTokens,
		OutputTokens:    logEntry.OutputTokens,
		ReasoningTokens: logEntry.ReasoningTokens,
		CacheReadTokens: logEntry.CacheReadTokens,
		ServiceTier:     modelpricing.ServiceTier(strings.ToLower(strings.TrimSpace(logEntry.ServiceTier))),
	}
	cost := ls.calculateCost(costs, logEntry.Provider, logEntry.Model, usage)
	logEntry.HasPricing = cost.HasPricing
	logEntry.InputCost = cost.InputCost
	logEntry.OutputCost = cost.OutputCost
	logEntry.ReasoningCost = cost.ReasoningCost
	logEntry.CacheReadCost = cost.CacheReadCost
	logEntry.TotalCost = cost.TotalCost
}

func (ls *LogService) calculateCost(
	costs *costContext,
	provider string,
	model string,
	usage modelpricing.UsageSnapshot,
) modelpricing.CostBreakdown {
	if ls == nil || ls.pricing == nil {
		return modelpricing.CostBreakdown{}
	}
	cost := ls.pricing.CalculateCost(model, usage)
	return applyCostMultiplier(cost, costs.multiplierFor(provider))
}

func applyCostMultiplier(cost modelpricing.CostBreakdown, multiplier float64) modelpricing.CostBreakdown {
	if multiplier == 0 || multiplier == 1 {
		return cost
	}
	cost.InputCost *= multiplier
	cost.OutputCost *= multiplier
	cost.ReasoningCost *= multiplier
	cost.CacheCreateCost *= multiplier
	cost.CacheReadCost *= multiplier
	cost.TotalCost *= multiplier
	return cost
}

func parseCreatedAt(record xdb.Record) (time.Time, bool) {
	if t := record.GetTime("created_at"); t != nil {
		return t.In(time.Local), true
	}
	raw := strings.TrimSpace(record.GetString("created_at"))
	if raw == "" {
		return time.Time{}, false
	}

	layouts := []string{
		timeLayout,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05 MST",
		"2006-01-02T15:04:05-0700",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.In(time.Local), true
		}
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return parsed.In(time.Local), true
		}
	}

	if normalized := strings.Replace(raw, " ", "T", 1); normalized != raw {
		if parsed, err := time.Parse(time.RFC3339, normalized); err == nil {
			return parsed.In(time.Local), true
		}
	}

	if len(raw) >= len("2006-01-02") {
		if parsed, err := time.ParseInLocation("2006-01-02", raw[:10], time.Local); err == nil {
			return parsed, false
		}
	}

	return time.Time{}, false
}

func parseTimeInput(value string) (time.Time, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return startOfDay(time.Now()), nil
	}
	layouts := []string{
		time.RFC3339,
		timeLayout,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05 MST",
		"2006-01-02T15:04:05-0700",
	}
	for _, layout := range layouts {
		// 前端传入的裸时间串是本地墙钟时间,必须先按本地时区解析;
		// time.Parse 会把无时区的串按 UTC 解释,只留作兜底
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return parsed.In(time.Local), nil
		}
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.In(time.Local), nil
		}
	}
	if normalized := strings.Replace(raw, " ", "T", 1); normalized != raw {
		if parsed, err := time.Parse(time.RFC3339, normalized); err == nil {
			return parsed.In(time.Local), nil
		}
	}
	if len(raw) >= len("2006-01-02") {
		if parsed, err := time.ParseInLocation("2006-01-02", raw[:10], time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time format: %s", raw)
}

func dayFromTimestamp(value string) string {
	if len(value) >= len("2006-01-02") {
		if t, err := time.ParseInLocation(timeLayout, value, time.Local); err == nil {
			return t.Format("2006-01-02")
		}
		return value[:10]
	}
	return value
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func startOfHour(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, t.Hour(), 0, 0, 0, t.Location())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isNoSuchTableErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no such table")
}

type HeatmapStat struct {
	Day             string  `json:"day"`
	TotalRequests   int64   `json:"total_requests"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	ReasoningTokens int64   `json:"reasoning_tokens"`
	TotalCost       float64 `json:"total_cost"`
}

type LogStats struct {
	TotalRequests   int64            `json:"total_requests"`
	InputTokens     int64            `json:"input_tokens"`
	OutputTokens    int64            `json:"output_tokens"`
	ReasoningTokens int64            `json:"reasoning_tokens"`
	CacheReadTokens int64            `json:"cache_read_tokens"`
	CostTotal       float64          `json:"cost_total"`
	CostInput       float64          `json:"cost_input"`
	CostOutput      float64          `json:"cost_output"`
	CostCacheRead   float64          `json:"cost_cache_read"`
	Series          []LogStatsSeries `json:"series"`
}

type ProviderDailyStat struct {
	Provider           string  `json:"provider"`
	TotalRequests      int64   `json:"total_requests"`
	SuccessfulRequests int64   `json:"successful_requests"`
	FailedRequests     int64   `json:"failed_requests"`
	SuccessRate        float64 `json:"success_rate"`
	InputTokens        int64   `json:"input_tokens"`
	OutputTokens       int64   `json:"output_tokens"`
	ReasoningTokens    int64   `json:"reasoning_tokens"`
	CacheReadTokens    int64   `json:"cache_read_tokens"`
	CostTotal          float64 `json:"cost_total"`
}

type LogStatsSeries struct {
	Day             string  `json:"day"`
	TotalRequests   int64   `json:"total_requests"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	ReasoningTokens int64   `json:"reasoning_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
	TotalCost       float64 `json:"total_cost"`
}
