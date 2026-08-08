package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

var ErrProviderConfigConflict = errors.New("provider configuration changed")

// AvailabilityConfig 可用性监控高级配置
// 在可用性页面的"高级配置"弹窗中设置，可选
type AvailabilityConfig struct {
	TestModel           string `json:"testModel,omitempty"`           // 覆盖默认测试模型
	TestEndpoint        string `json:"testEndpoint,omitempty"`        // 覆盖默认测试端点
	Timeout             int    `json:"timeout,omitempty"`             // 覆盖默认超时（毫秒）
	PollIntervalSeconds int    `json:"pollIntervalSeconds,omitempty"` // 后台检测间隔（秒）
}

// SanitizeConfig 请求清理高级配置（黑名单模式）。
// 三个列表用指针区分三态：nil 指针（字段缺失/null）= 用内置默认黑名单；
// 指向空数组 = 该维度什么都不删；非空 = 用自定义列表。
type SanitizeConfig struct {
	BlockedBodyFields *[]string `json:"blockedBodyFields,omitempty"` // 要移除的请求体顶层字段
	BlockedHeaders    *[]string `json:"blockedHeaders,omitempty"`    // 要移除的请求头（小写）
}

// cloneStringListPtr 深拷贝三态列表指针：nil 保持 nil，非 nil 复制底层数组。
func cloneStringListPtr(p *[]string) *[]string {
	if p == nil {
		return nil
	}
	v := append([]string{}, (*p)...)
	return &v
}

type Provider struct {
	ID      int64  `json:"id"` // 修复：使用 int64 支持大 ID 值
	Name    string `json:"name"`
	APIURL  string `json:"apiUrl"`
	APIKey  string `json:"apiKey"`
	Site    string `json:"officialSite"`
	Icon    string `json:"icon"`
	Tint    string `json:"tint"`
	Accent  string `json:"accent"`
	Enabled bool   `json:"enabled"`

	// API 端点路径（可选）- 覆盖平台默认端点
	// 如：兼容网关可能需要使用 /v1/chat/completions 而非 /responses。
	// 留空则使用 Codex 默认端点 /responses。
	APIEndpoint string `json:"apiEndpoint,omitempty"`

	// 备用 API 地址（可选，最多 4 个）- 同一供应商的多入口容灾
	// 主地址（APIURL）失败且属可切换错误时，同一请求内按序改试备用地址；
	// 全部失败才算该供应商一次失败。
	FallbackAPIURLs []string `json:"fallbackApiUrls,omitempty"`

	// 已废弃的路由字段只保留源码兼容性。模型归属、顺序和并发策略均由
	// ModelGroup 决定；旧配置中的这些字段会在下次保存时被丢弃。
	MaxConcurrency  int             `json:"-"`
	SupportedModels map[string]bool `json:"-"`

	// 模型映射 - 外部模型名 -> Provider 内部模型名
	// 支持精确匹配和通配符（如 "gpt-*" -> "gateway/gpt-*"）。
	ModelMapping map[string]string `json:"modelMapping,omitempty"`

	Level int `json:"-"`

	// 费用倍率 - 影响 WebUI 费用统计和每日费用限额，0 表示旧配置的默认倍率 1。
	CostMultiplier float64 `json:"costMultiplier,omitempty"`

	// 每日费用限额按微美元存储（1 USD = 1,000,000 micro-USD）。
	// 独立状态服务负责当日封禁，不会修改 Provider.Enabled。
	DailyCostLimitEnabled bool  `json:"dailyCostLimitEnabled,omitempty"`
	DailyCostLimitMicros  int64 `json:"dailyCostLimitMicros,omitempty"`

	// ========== 可用性监控字段（新增 v0.5.0） ==========

	// 可用性监控开关 - 在可用性页面配置
	// 启用后才会执行后台健康检查
	AvailabilityMonitorEnabled bool `json:"availabilityMonitorEnabled,omitempty"`

	// 连通性自动拉黑开关 - 在 Provider 编辑页面配置
	// 前置条件：AvailabilityMonitorEnabled 必须为 true
	// 启用后，健康检查连续失败达到阈值时向拉黑服务上报失败计数，
	// 由拉黑服务按其自身失败阈值决定何时真正拉黑
	ConnectivityAutoBlacklist bool `json:"connectivityAutoBlacklist,omitempty"`

	// 可用时自动解禁 - 与自动拉黑相互独立；普通黑名单内连续两次探测成功后提前恢复。
	// 每日额度封禁由独立服务管理，不受该开关影响。
	AvailabilityAutoUnblock bool `json:"availabilityAutoUnblock,omitempty"`

	// 可用性高级配置 - 可选，在可用性页面的"高级配置"中设置
	AvailabilityConfig *AvailabilityConfig `json:"availabilityConfig,omitempty"`

	// 认证方式 - bearer / x-api-key / 自定义 Header 名
	// 空值时使用 Codex 默认的 bearer 认证。
	ConnectivityAuthType string `json:"connectivityAuthType,omitempty"`

	// 跳过上游 TLS 证书验证 - 仅对该供应商生效（自签名证书/企业代理场景）
	// 默认 false；开启后该供应商的转发与健康/连通性探测都不再校验证书，存在中间人风险
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// 请求清理开关 - 启用后转发前移除非标准字段和不支持的请求头
	// 解决 LiteLLM 等中转服务的 "Extra inputs are not permitted" 兼容性问题
	RequestSanitizeEnabled bool            `json:"requestSanitizeEnabled,omitempty"`
	SanitizeConfig         *SanitizeConfig `json:"sanitizeConfig,omitempty"`

	// ========== 旧字段（已废弃，仅用于读取迁移） ==========
	// 这些字段在保存时不再写入，但读取时会自动迁移到新字段

	// [已废弃] 连通性检测开关 - 迁移到 AvailabilityMonitorEnabled
	ConnectivityCheck bool `json:"connectivityCheck,omitempty"`

	// [已废弃] 连通性检测模型 - 迁移到 AvailabilityConfig.TestModel
	ConnectivityTestModel string `json:"connectivityTestModel,omitempty"`

	// [已废弃] 连通性检测端点 - 迁移到 AvailabilityConfig.TestEndpoint
	ConnectivityTestEndpoint string `json:"connectivityTestEndpoint,omitempty"`

	// 内部字段：配置验证错误（不持久化）
	configErrors []string `json:"-"`
}

type ModelGroup struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Priority    int      `json:"priority"`
	Models      []string `json:"models"`
	ProviderIDs []int64  `json:"providerIds"`
}

type providerEnvelope struct {
	Providers []Provider `json:"providers"`
	// nil means the file predates model groups; a non-nil empty slice means the
	// user deliberately deleted every group.
	ModelGroups []ModelGroup `json:"modelGroups"`

	// removedRoutingFields is set while decoding legacy Provider routing
	// fields so the normalized configuration can be persisted immediately.
	removedRoutingFields bool `json:"-"`
}

const defaultModelGroupName = "Default"

func defaultModelGroup(providers []Provider) ModelGroup {
	providerIDs := make([]int64, 0, len(providers))
	for _, provider := range providers {
		providerIDs = append(providerIDs, provider.ID)
	}
	return ModelGroup{
		ID:          1,
		Name:        defaultModelGroupName,
		Enabled:     true,
		Priority:    100,
		Models:      []string{"*"},
		ProviderIDs: providerIDs,
	}
}

func ensureModelGroups(groups []ModelGroup, providers []Provider) ([]ModelGroup, bool) {
	if groups != nil {
		return groups, false
	}
	return []ModelGroup{defaultModelGroup(providers)}, true
}

func normalizeModelGroups(groups []ModelGroup, providers []Provider) []ModelGroup {
	providerIDs := make(map[int64]struct{}, len(providers))
	for _, provider := range providers {
		providerIDs[provider.ID] = struct{}{}
	}
	normalized := make([]ModelGroup, len(groups))
	for index, group := range groups {
		group.Name = strings.TrimSpace(group.Name)
		models := make([]string, 0, len(group.Models))
		for _, model := range group.Models {
			model = strings.TrimSpace(model)
			if model != "" {
				models = append(models, model)
			}
		}
		group.Models = models
		seen := make(map[int64]struct{}, len(group.ProviderIDs))
		ids := make([]int64, 0, len(group.ProviderIDs))
		for _, id := range group.ProviderIDs {
			if _, exists := providerIDs[id]; !exists {
				continue
			}
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		group.ProviderIDs = ids
		normalized[index] = group
	}
	return normalized
}

func validateModelGroups(groups []ModelGroup) error {
	ids := make(map[int64]struct{}, len(groups))
	names := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		if group.ID <= 0 {
			return fmt.Errorf("模型分组 ID 必须大于 0")
		}
		if _, exists := ids[group.ID]; exists {
			return fmt.Errorf("模型分组 ID %d 重复", group.ID)
		}
		ids[group.ID] = struct{}{}
		name := strings.TrimSpace(group.Name)
		if name == "" {
			return fmt.Errorf("模型分组名称不能为空")
		}
		nameKey := strings.ToLower(name)
		if _, exists := names[nameKey]; exists {
			return fmt.Errorf("模型分组名称 %q 重复", name)
		}
		names[nameKey] = struct{}{}
		if group.Priority < 1 || group.Priority > 100 {
			return fmt.Errorf("模型分组 %q 的优先级必须在 1-100 之间", name)
		}
		for _, pattern := range group.Models {
			if strings.Count(pattern, "*") > 1 {
				return fmt.Errorf("模型分组 %q 的规则 %q 最多只能包含一个 *", name, pattern)
			}
		}
	}
	return nil
}

func (group ModelGroup) IsComplete() bool {
	return strings.TrimSpace(group.Name) != "" && len(group.Models) > 0 && len(group.ProviderIDs) > 0
}

func (group ModelGroup) MatchesModel(model string) bool {
	for _, pattern := range group.Models {
		if matchWildcard(pattern, model) {
			return true
		}
	}
	return false
}

// OrderedModelGroups applies routing priority while preserving the stored
// order for ties. The stored order is the manual drag order from the WebUI.
func OrderedModelGroups(groups []ModelGroup) []ModelGroup {
	ordered := append([]ModelGroup(nil), groups...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Priority < ordered[j].Priority
	})
	return ordered
}

func SelectModelGroup(groups []ModelGroup, model string) *ModelGroup {
	for _, group := range OrderedModelGroups(groups) {
		if group.Enabled && group.IsComplete() && group.MatchesModel(model) {
			selected := group
			return &selected
		}
	}
	return nil
}

func ProvidersForModelGroup(providers []Provider, group ModelGroup) []Provider {
	byID := make(map[int64]Provider, len(providers))
	for _, provider := range providers {
		byID[provider.ID] = provider
	}
	ordered := make([]Provider, 0, len(group.ProviderIDs))
	for _, id := range group.ProviderIDs {
		if provider, exists := byID[id]; exists {
			ordered = append(ordered, provider)
		}
	}
	return ordered
}

type ProviderService struct {
	mu sync.Mutex
	// configGen 配置代数：每次成功落盘递增，供 WebUI 乐观并发控制
	// 和后台健康检查感知配置刷新。
	configGen atomic.Int64
}

// configGeneration 返回当前配置代数（供转发路径在装载供应商时捕获）
func (ps *ProviderService) configGeneration() int64 {
	return ps.configGen.Load()
}

func NewProviderService() *ProviderService {
	return &ProviderService{}
}

func (ps *ProviderService) Start() error { return nil }
func (ps *ProviderService) Stop() error  { return nil }

func providerFilePath(kind string) (string, error) {
	if err := requireCodexPlatform(kind); err != nil {
		return "", err
	}
	dir, err := getUserConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, CodexPlatform+".json"), nil
}

func (ps *ProviderService) SaveProviders(kind string, providers []Provider) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.saveProvidersLocked(kind, providers)
}

// UpdateProviders applies a WebUI snapshot while holding the same lock used by
// every other provider mutation. The generation check prevents a stale browser
// from silently overwriting changes made by another request or background job.
func (ps *ProviderService) UpdateProviders(
	kind string,
	expectedGeneration int64,
	update func([]Provider) ([]Provider, error),
) ([]Provider, int64, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	providers, groups, groupsInitialized, err := ps.loadConfigurationNoLock(kind)
	if err != nil {
		return nil, ps.configGen.Load(), err
	}
	currentGeneration := ps.configGen.Load()
	if currentGeneration != expectedGeneration {
		return nil, currentGeneration, fmt.Errorf(
			"%w: expected generation %d, current generation %d",
			ErrProviderConfigConflict,
			expectedGeneration,
			currentGeneration,
		)
	}

	updated, err := update(providers)
	if err != nil {
		return nil, currentGeneration, err
	}
	if !groupsInitialized {
		groups = []ModelGroup{defaultModelGroup(updated)}
	}
	if err := ps.saveConfigurationLocked(kind, updated, groups); err != nil {
		return nil, currentGeneration, err
	}
	return updated, ps.configGen.Load(), nil
}

func (ps *ProviderService) UpdateModelGroups(kind string, expectedGeneration int64, groups []ModelGroup) (int64, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	providers, _, _, err := ps.loadConfigurationNoLock(kind)
	if err != nil {
		return ps.configGen.Load(), err
	}
	currentGeneration := ps.configGen.Load()
	if currentGeneration != expectedGeneration {
		return currentGeneration, fmt.Errorf(
			"%w: expected generation %d, current generation %d",
			ErrProviderConfigConflict,
			expectedGeneration,
			currentGeneration,
		)
	}
	if groups == nil {
		groups = []ModelGroup{}
	}
	if err := ps.saveConfigurationLocked(kind, providers, groups); err != nil {
		return currentGeneration, err
	}
	return ps.configGen.Load(), nil
}

// mutateProviders 在锁内完成"加载→修改→保存"的整段读改写。
// 供跨调用修改 provider 字段的场景使用(如健康检查开关),
// 避免调用方拆分 Load/Save 导致并发保存相互覆盖丢失更新。
func (ps *ProviderService) mutateProviders(kind string, mutate func(providers []Provider) ([]Provider, error)) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	providers, groups, groupsInitialized, err := ps.loadConfigurationNoLock(kind)
	if err != nil {
		return err
	}
	updated, err := mutate(providers)
	if err != nil {
		return err
	}
	if !groupsInitialized {
		groups = []ModelGroup{defaultModelGroup(updated)}
	}
	return ps.saveConfigurationLocked(kind, updated, groups)
}

func (ps *ProviderService) loadEnvelopeRaw(kind string) (providerEnvelope, error) {
	path, err := providerFilePath(kind)
	if err != nil {
		return providerEnvelope{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return providerEnvelope{}, nil
		}
		return providerEnvelope{}, err
	}

	var envelope providerEnvelope
	if len(data) == 0 {
		return envelope, nil
	}

	if err := json.Unmarshal(data, &envelope); err != nil {
		return providerEnvelope{}, err
	}
	var raw struct {
		Providers []map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return providerEnvelope{}, err
	}
	for _, provider := range raw.Providers {
		for _, field := range []string{"level", "supportedModels", "maxConcurrency"} {
			if _, exists := provider[field]; exists {
				envelope.removedRoutingFields = true
				break
			}
		}
		if envelope.removedRoutingFields {
			break
		}
	}
	return envelope, nil
}

// loadProvidersRaw 原样读取配置文件（不迁移、不保存）。
func (ps *ProviderService) loadProvidersRaw(kind string) ([]Provider, error) {
	envelope, err := ps.loadEnvelopeRaw(kind)
	return envelope.Providers, err
}

// saveProvidersLocked 内部保存方法，调用方必须已持有锁
func (ps *ProviderService) saveProvidersLocked(kind string, providers []Provider) error {
	existing, err := ps.loadEnvelopeRaw(kind)
	if err != nil {
		return err
	}
	groups, _ := ensureModelGroups(existing.ModelGroups, providers)
	return ps.saveConfigurationLocked(kind, providers, groups)
}

// saveConfigurationLocked is the single writer for providers and model groups.
// Keeping both collections in one envelope makes provider deletion and group
// reference pruning atomic.
func (ps *ProviderService) saveConfigurationLocked(kind string, providers []Provider, groups []ModelGroup) error {
	path, err := providerFilePath(kind)
	if err != nil {
		return err
	}

	// 加载现有配置，用于检查 name 是否被修改
	// 使用原样读取，避免触发迁移导致死锁
	existing, err := ps.loadEnvelopeRaw(kind)
	if err != nil {
		return err
	}
	existingProviders := existing.Providers
	nameByID := make(map[int64]string, len(existingProviders))
	for _, p := range existingProviders {
		nameByID[p.ID] = p.Name
	}

	// 解析 platform（alias 校验需要）。
	aliasPlatform, aliasErr := resolvePlatform(kind)
	// 规则:名字不得占用其他 provider 的 48h 活动 alias
	// 防止 "A→B 后新建同名 A" 被 alias resolver 静默归并到 B 的历史里。
	// 一次性批量查询：旧实现在持锁期间对每个 provider 单发一条 SELECT，
	// 拖拽排序保存 N 个供应商就是 N 次串行 DB 往返，全程压着 ps.mu
	if aliasErr == nil {
		if err := checkNamesNotOccupiedByAlias(aliasPlatform, providers); err != nil {
			return err
		}
	}

	// 验证每个 provider 的配置，并清除旧字段
	validationErrors := make([]string, 0)
	for i := range providers {
		p := &providers[i]

		// 规则：name 不可修改（走独立 RenameProvider 路径,SaveProviders 只允许既有 name）
		if oldName, ok := nameByID[p.ID]; ok && oldName != p.Name {
			return fmt.Errorf("provider id %d 的 name 不可修改(请使用 RenameProvider)", p.ID)
		}

		// 验证 Provider 自身配置。模型归属由分组校验负责。
		if errs := p.ValidateConfiguration(); len(errs) > 0 {
			for _, errMsg := range errs {
				validationErrors = append(validationErrors, fmt.Sprintf("[%s] %s", p.Name, errMsg))
			}
		}

		// 清除旧连通性字段，确保保存时不再写入
		p.clearLegacyFields()
	}

	// 如果有验证错误，返回汇总错误
	if len(validationErrors) > 0 {
		return fmt.Errorf("配置验证失败：\n  - %s", strings.Join(validationErrors, "\n  - "))
	}

	groups = normalizeModelGroups(groups, providers)
	if err := validateModelGroups(groups); err != nil {
		return err
	}
	if groups == nil {
		groups = []ModelGroup{}
	}
	data, err := json.MarshalIndent(providerEnvelope{Providers: providers, ModelGroups: groups}, "", "  ")
	if err != nil {
		return err
	}

	if err := atomicWriteFile(path, data, 0o600); err != nil {
		return err
	}
	// 配置代数递增，供 WebUI 的乐观并发控制使用。
	ps.configGen.Add(1)
	return nil
}

// LoadProvidersWithGen 返回配对一致的 (providers, 配置代数)。
// 必须与写入方共用 ps.mu：
// 写入方"改名文件→递增代数"两步之间存在窗口，锁外读取可能拿到
// (新配置, 旧代数)。
// 锁内用 loadProvidersNoLock（其迁移保存不再加锁），无递归死锁。
func (ps *ProviderService) LoadProvidersWithGen(kind string) ([]Provider, int64, error) {
	providers, _, generation, err := ps.LoadConfigurationWithGen(kind)
	return providers, generation, err
}

func (ps *ProviderService) LoadConfigurationWithGen(kind string) ([]Provider, []ModelGroup, int64, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	providers, groups, _, err := ps.loadConfigurationNoLock(kind)
	gen := ps.configGen.Load()
	return providers, groups, gen, err
}

func (ps *ProviderService) LoadProviders(kind string) ([]Provider, error) {
	providers, _, _, err := ps.LoadConfigurationWithGen(kind)
	return providers, err
}

func (ps *ProviderService) loadConfigurationNoLock(kind string) ([]Provider, []ModelGroup, bool, error) {
	envelope, err := ps.loadEnvelopeRaw(kind)
	if err != nil {
		return nil, nil, false, err
	}
	groupsInitialized := envelope.ModelGroups != nil
	groups, groupMigrated := ensureModelGroups(envelope.ModelGroups, envelope.Providers)
	migrated := envelope.removedRoutingFields
	for i := range envelope.Providers {
		if envelope.Providers[i].migrateFromLegacy() {
			migrated = true
		}
	}
	// Do not persist an empty fresh configuration yet: the first provider save
	// must be able to seed the default group with that provider.
	if migrated || (groupMigrated && len(envelope.Providers) > 0) {
		if err := ps.saveConfigurationLocked(kind, envelope.Providers, groups); err != nil {
			log.Printf("[ProviderService] 锁内迁移保存失败: %v\n", err)
		} else {
			groupsInitialized = true
		}
	}
	return envelope.Providers, groups, groupsInitialized, nil
}

func (ps *ProviderService) loadProvidersNoLock(kind string) ([]Provider, error) {
	providers, _, _, err := ps.loadConfigurationNoLock(kind)
	return providers, err
}

// migrateFromLegacy 将旧连通性字段迁移到新可用性字段
// 返回 true 表示发生了迁移
func (p *Provider) migrateFromLegacy() bool {
	migrated := false

	// 迁移 ConnectivityCheck -> AvailabilityMonitorEnabled
	// 仅当新字段未设置（false）且旧字段已设置（true）时迁移
	if p.ConnectivityCheck && !p.AvailabilityMonitorEnabled {
		p.AvailabilityMonitorEnabled = true
		migrated = true
	}

	// 迁移测试模型和端点到 AvailabilityConfig
	if p.ConnectivityTestModel != "" || p.ConnectivityTestEndpoint != "" {
		if p.AvailabilityConfig == nil {
			p.AvailabilityConfig = &AvailabilityConfig{}
		}
		// 仅当新字段为空时才从旧字段迁移
		if p.AvailabilityConfig.TestModel == "" && p.ConnectivityTestModel != "" {
			p.AvailabilityConfig.TestModel = p.ConnectivityTestModel
			migrated = true
		}
		if p.AvailabilityConfig.TestEndpoint == "" && p.ConnectivityTestEndpoint != "" {
			p.AvailabilityConfig.TestEndpoint = p.ConnectivityTestEndpoint
			migrated = true
		}
	}

	return migrated
}

// clearLegacyFields 清除旧字段值，使其在序列化时被 omitempty 跳过
func (p *Provider) clearLegacyFields() {
	p.ConnectivityCheck = false
	p.ConnectivityTestModel = ""
	p.ConnectivityTestEndpoint = ""
	// 注意：ConnectivityAuthType 现在是活跃字段，不再清除
}

// uniqueProviderName 保证生成的供应商名字在现有列表中唯一。
// 名字冲突时追加序号（"X (副本) 2"）：黑名单、用量统计与别名迁移都以 name 为键，
// 重名会让两个供应商的状态互相串扰。
func uniqueProviderName(providers []Provider, candidate string) string {
	taken := make(map[string]bool, len(providers))
	for _, p := range providers {
		taken[strings.ToLower(strings.TrimSpace(p.Name))] = true
	}

	name := candidate
	for seq := 2; taken[strings.ToLower(strings.TrimSpace(name))]; seq++ {
		name = fmt.Sprintf("%s %d", candidate, seq)
	}
	return name
}

// DuplicateProvider 复制供应商配置，生成新的副本
// 返回新创建的 Provider 对象
func (ps *ProviderService) DuplicateProvider(kind string, sourceID int64) (*Provider, error) {
	// 1. 先加锁，避免并发修改
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// 2. 加载现有配置（在锁内完成，确保数据一致性）
	// 注意：LoadProviders 内部可能触发迁移保存，会再次尝试加锁导致死锁
	// 因此使用不加锁的内部加载逻辑
	providers, err := ps.loadProvidersNoLock(kind)
	if err != nil {
		return nil, fmt.Errorf("加载供应商配置失败: %w", err)
	}

	// 3. 查找源供应商
	var source *Provider
	for i := range providers {
		if providers[i].ID == sourceID {
			source = &providers[i]
			break
		}
	}
	if source == nil {
		return nil, fmt.Errorf("未找到 ID 为 %d 的供应商", sourceID)
	}

	// 4. 生成新 ID，避开活动 alias 仍占用的历史 ID
	platform, err := resolvePlatform(kind)
	if err != nil {
		return nil, err
	}
	newID, err := nextAvailableProviderID(platform, providers)
	if err != nil {
		return nil, fmt.Errorf("生成副本 ID 失败: %w", err)
	}

	// 5. 克隆配置（深拷贝）
	// 名字必须在现有列表里唯一：黑名单、用量统计、别名迁移全部以 name 为键，
	// 同名供应商会互相串扰（一个被拉黑，另一个也被跳过；用量记到同一条）
	cloned := &Provider{
		ID:                    newID,
		Name:                  uniqueProviderName(providers, source.Name+" (副本)"),
		APIURL:                source.APIURL,
		APIKey:                source.APIKey,
		Site:                  source.Site,
		Icon:                  source.Icon,
		Tint:                  source.Tint,
		Accent:                source.Accent,
		Enabled:               false, // 默认禁用，避免与源供应商冲突
		CostMultiplier:        source.CostMultiplier,
		DailyCostLimitEnabled: source.DailyCostLimitEnabled,
		DailyCostLimitMicros:  source.DailyCostLimitMicros,
		APIEndpoint:           source.APIEndpoint,          // 复制端点配置
		ConnectivityAuthType:  source.ConnectivityAuthType, // 复制认证方式
		InsecureSkipVerify:    source.InsecureSkipVerify,   // 复制 TLS 跳验开关
		// 请求清理配置
		RequestSanitizeEnabled: source.RequestSanitizeEnabled,
		// 可用性监控配置
		AvailabilityMonitorEnabled: source.AvailabilityMonitorEnabled,
		ConnectivityAutoBlacklist:  false, // 副本默认关闭自动拉黑
		AvailabilityAutoUnblock:    false, // 副本默认关闭自动解禁
	}

	// 深拷贝 SanitizeConfig（指针三态列表逐个复制，避免副本与源共享底层数组）
	if source.SanitizeConfig != nil {
		cloned.SanitizeConfig = &SanitizeConfig{
			BlockedBodyFields: cloneStringListPtr(source.SanitizeConfig.BlockedBodyFields),
			BlockedHeaders:    cloneStringListPtr(source.SanitizeConfig.BlockedHeaders),
		}
	}

	// 深拷贝 AvailabilityConfig
	if source.AvailabilityConfig != nil {
		cloned.AvailabilityConfig = &AvailabilityConfig{
			TestModel:           source.AvailabilityConfig.TestModel,
			TestEndpoint:        source.AvailabilityConfig.TestEndpoint,
			Timeout:             source.AvailabilityConfig.Timeout,
			PollIntervalSeconds: source.AvailabilityConfig.PollIntervalSeconds,
		}
	}

	if source.ModelMapping != nil {
		cloned.ModelMapping = make(map[string]string, len(source.ModelMapping))
		for k, v := range source.ModelMapping {
			cloned.ModelMapping[k] = v
		}
	}

	if len(source.FallbackAPIURLs) > 0 {
		cloned.FallbackAPIURLs = append([]string(nil), source.FallbackAPIURLs...)
	}

	// 7. 添加到列表并保存（使用内部方法避免死锁）
	providers = append(providers, *cloned)
	if err := ps.saveProvidersLocked(kind, providers); err != nil {
		return nil, fmt.Errorf("保存副本失败: %w", err)
	}

	return cloned, nil
}

// modelInWhitelist 检查模型是否命中白名单（精确或通配符）。
// 值为 false 的条目视为未声明，精确与通配符路径都不算命中。
func modelInWhitelist(supported map[string]bool, modelName string) bool {
	if supported[modelName] {
		return true
	}
	for pattern, allowed := range supported {
		if allowed && matchWildcard(pattern, modelName) {
			return true
		}
	}
	return false
}

// modelSupportedBy 判断白名单/映射组合是否支持指定模型。
// Provider 的白名单与映射共用这一份逻辑。
func modelSupportedBy(supported map[string]bool, mapping map[string]string, modelName string) bool {
	// 向后兼容：如果未配置白名单和映射，假设支持所有模型
	if len(supported) == 0 && len(mapping) == 0 {
		return true
	}

	// 场景 A：原生支持该模型（精确或通配符）
	if modelInWhitelist(supported, modelName) {
		return true
	}

	// 场景 B：通过映射支持该模型（精确或通配符）
	if mapping != nil {
		if _, exists := mapping[modelName]; exists {
			return true
		}
		for pattern := range mapping {
			if matchWildcard(pattern, modelName) {
				return true
			}
		}
	}

	// 场景 C：不支持
	return false
}

// effectiveModelFor 返回映射后的模型名；无命中映射时原样返回。
// 精确映射优先；多条通配符同时命中时按"字面量最长优先，等长按字典序"
// 确定性取胜——Go map 迭代序随机，不定序会让同一份配置在不同进程里
// 把同一个请求改写成不同模型。
func effectiveModelFor(mapping map[string]string, requestedModel string) string {
	if len(mapping) == 0 {
		return requestedModel
	}

	// 优先查找精确映射
	if mappedModel, exists := mapping[requestedModel]; exists {
		return mappedModel
	}

	// 通配符映射：收集所有命中项后确定性选择
	bestPattern := ""
	bestLiteral := -1
	for pattern := range mapping {
		if !matchWildcard(pattern, requestedModel) {
			continue
		}
		literal := len(strings.ReplaceAll(pattern, "*", ""))
		if literal > bestLiteral || (literal == bestLiteral && pattern < bestPattern) {
			bestPattern = pattern
			bestLiteral = literal
		}
	}
	if bestPattern == "" {
		return requestedModel
	}
	return applyWildcardMapping(bestPattern, mapping[bestPattern], requestedModel)
}

// IsModelSupported 检查 provider 是否支持指定的模型
// 支持条件：1) 模型在 SupportedModels 中（精确或通配符匹配）
//  2. 模型在 ModelMapping 的 key 中（精确或通配符匹配）
func (p *Provider) IsModelSupported(modelName string) bool {
	return modelSupportedBy(p.SupportedModels, p.ModelMapping, modelName)
}

// GetEffectiveModel 获取实际应该使用的模型名
// 如果存在映射（精确或通配符），返回映射后的模型名；否则返回原模型名
func (p *Provider) GetEffectiveModel(requestedModel string) string {
	return effectiveModelFor(p.ModelMapping, requestedModel)
}

// GetEffectiveEndpoint 获取有效的 API 端点
// 优先使用用户配置的端点，否则使用平台默认
func (p *Provider) GetEffectiveEndpoint(defaultEndpoint string) string {
	ep := strings.TrimSpace(p.APIEndpoint)
	if ep == "" {
		return defaultEndpoint
	}

	// 校验：必须是相对路径，不能是完整 URL
	if strings.HasPrefix(ep, "http://") || strings.HasPrefix(ep, "https://") {
		log.Printf("[Provider] 警告: apiEndpoint 应该是相对路径（如 /v1/chat/completions），而非完整 URL: %s，使用默认端点", ep)
		return defaultEndpoint
	}

	// 确保以 / 开头
	if !strings.HasPrefix(ep, "/") {
		ep = "/" + ep
	}

	return ep
}

// validateModelConfig 校验模型映射，返回错误列表（空表示通过）。
// supported 参数仅为旧包内调用保留，Provider 模型归属现由 ModelGroup 决定。
func validateModelConfig(_ map[string]bool, mapping map[string]string) []string {
	errors := make([]string, 0)

	for externalModel, internalModel := range mapping {
		// 空目标会把请求模型改写成空串，上游必拒，直接判为配置错误
		if strings.TrimSpace(internalModel) == "" {
			errors = append(errors, fmt.Sprintf(
				"模型映射无效：'%s' 的目标模型为空", externalModel))
			continue
		}

	}

	return errors
}

// ValidateConfiguration 验证 Provider 配置
// 返回验证错误列表（空则表示验证通过）
func (p *Provider) ValidateConfiguration() []string {
	errors := validateModelConfig(nil, p.ModelMapping)
	errors = append(errors, validateFallbackURLs(p.FallbackAPIURLs)...)
	if err := validateCostMultiplier(p.CostMultiplier); err != nil {
		errors = append(errors, err.Error())
	}
	if p.DailyCostLimitMicros < 0 || p.DailyCostLimitMicros > maxMoneyMicros {
		errors = append(errors, fmt.Sprintf("每日费用限额必须在 0-%d 微美元之间", maxMoneyMicros))
	} else if p.DailyCostLimitEnabled && p.DailyCostLimitMicros <= 0 {
		errors = append(errors, "启用每日费用限额时，限额必须大于 0")
	}
	if p.AvailabilityConfig != nil {
		interval := p.AvailabilityConfig.PollIntervalSeconds
		if interval != 0 && (interval < MinAvailabilityPollIntervalSeconds || interval > MaxAvailabilityPollIntervalSeconds) {
			errors = append(errors, fmt.Sprintf("可用性检测间隔必须在 %d-%d 秒之间", MinAvailabilityPollIntervalSeconds, MaxAvailabilityPollIntervalSeconds))
		}
	}
	p.configErrors = errors
	return errors
}

// EffectiveAvailabilityPollIntervalSeconds preserves Provider files created
// before per-provider polling was introduced. Invalid hand-edited values fall
// back to the default instead of creating a tight scheduler loop.
func (p Provider) EffectiveAvailabilityPollIntervalSeconds() int {
	if p.AvailabilityConfig == nil {
		return DefaultAvailabilityPollIntervalSeconds
	}
	interval := p.AvailabilityConfig.PollIntervalSeconds
	if interval < MinAvailabilityPollIntervalSeconds || interval > MaxAvailabilityPollIntervalSeconds {
		return DefaultAvailabilityPollIntervalSeconds
	}
	return interval
}

func validateCostMultiplier(multiplier float64) error {
	if multiplier == 0 {
		return nil
	}
	if math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier < 0.01 || multiplier > 100 {
		return fmt.Errorf("费用倍率必须在 0.01-100 之间")
	}
	return nil
}

// EffectiveCostMultiplier preserves old Provider files that predate the field.
func (p Provider) EffectiveCostMultiplier() float64 {
	if p.CostMultiplier == 0 {
		return 1
	}
	return p.CostMultiplier
}

// matchWildcard 通配符匹配函数
// 支持 * 通配符，如 "gpt-*" 匹配 "gpt-5.6"
func matchWildcard(pattern, text string) bool {
	// 如果没有通配符，使用精确匹配
	if !strings.Contains(pattern, "*") {
		return pattern == text
	}

	// 简化实现：只支持单个 * 通配符
	parts := strings.Split(pattern, "*")
	if len(parts) == 2 {
		// 前缀 + * 或 * + 后缀
		prefix, suffix := parts[0], parts[1]
		// * 匹配 0 个及以上字符：text 长度不足以同时容纳前后缀时不可能命中。
		// 漏掉该检查时 "gpt-*-pro" 会错误匹配 "gpt-pro"（前后缀在 text
		// 中重叠），随后 applyWildcardMapping 的切片会越界 panic
		if len(text) < len(prefix)+len(suffix) {
			return false
		}
		return strings.HasPrefix(text, prefix) && strings.HasSuffix(text, suffix)
	}

	// 多个 * 的情况（更复杂，暂不支持）
	return false
}

// applyWildcardMapping 应用通配符映射
// 将 pattern 中的 * 匹配部分替换到 replacement 的 * 位置
// 示例: pattern="gpt-*", replacement="gateway/gpt-*", input="gpt-5.6"
//
//	输出: "gateway/gpt-5.6"
func applyWildcardMapping(pattern, replacement, input string) string {
	// 如果 pattern 或 replacement 没有通配符，直接返回 replacement
	if !strings.Contains(pattern, "*") || !strings.Contains(replacement, "*") {
		return replacement
	}

	// 提取通配符匹配的部分
	parts := strings.Split(pattern, "*")
	if len(parts) != 2 {
		return replacement // 不支持多个通配符
	}

	prefix, suffix := parts[0], parts[1]

	// 验证 input 确实匹配 pattern（含长度检查，防止重叠切片越界）
	if len(input) < len(prefix)+len(suffix) ||
		!strings.HasPrefix(input, prefix) || !strings.HasSuffix(input, suffix) {
		return replacement
	}

	// 提取中间部分
	wildcardPart := input[len(prefix) : len(input)-len(suffix)]

	// 替换 replacement 中的 *
	return strings.Replace(replacement, "*", wildcardPart, 1)
}
