package services

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	modelpricing "codeswitch/resources/model-pricing"
)

// 默认模型策略从 OpenAI 目录解析 Codex 最新可用模型。
// 健康探测模型固定，不允许模型目录同步悄悄改变运行行为。
const (
	FallbackCodexDefaultModel = "gpt-5.6"
	FallbackCodexProbeModel   = "gpt-5.6-sol"
)

// resolverMaxFutureSkew release_date 允许的最大未来偏移(UTC),超过视为脏数据不入选。
const resolverMaxFutureSkew = 3 * 24 * time.Hour

// CatalogSource 提供当前生效的厂商目录(由模型同步服务实现)。
type CatalogSource interface {
	Catalogs() map[string]*modelpricing.RemoteCatalog
}

// DefaultModels 各平台当前解析结果,供前端与配置写入方使用。
type DefaultModels struct {
	CodexDefault string `json:"codexDefault"`
	CodexProbe   string `json:"codexProbe"`
}

// DefaultModelPolicy 解析入口。并发安全:source 替换与读取加锁,解析本身无状态。
type DefaultModelPolicy struct {
	mu      sync.RWMutex
	source  CatalogSource
	pricing *modelpricing.Service
	now     func() time.Time
}

// NewDefaultModelPolicy 创建策略;目录源由模型同步服务构造后注入(SetSource)。
func NewDefaultModelPolicy() *DefaultModelPolicy {
	pricing, _ := modelpricing.DefaultService()
	return &DefaultModelPolicy{pricing: pricing, now: time.Now}
}

// SetSource 注入目录源。
func (p *DefaultModelPolicy) SetSource(source CatalogSource) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.source = source
	p.mu.Unlock()
}

func (p *DefaultModelPolicy) catalog(providerID string) *modelpricing.RemoteCatalog {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	source := p.source
	p.mu.RUnlock()
	if source == nil {
		return nil
	}
	catalogs := source.Catalogs()
	if catalogs == nil {
		return nil
	}
	return catalogs[providerID]
}

// CodexDefaultModel 写入 Codex 配置的默认模型:
// 主线最大版本 M 与 codex 专线最大版本 V 比较,V>=M 才选专线,否则选主线。
func (p *DefaultModelPolicy) CodexDefaultModel() string {
	mainline, okMain := p.selectModel("openai", codexMainlinePattern, selectOpts{requireToolCall: true})
	codexLine, okCodex := p.selectModel("openai", codexLinePattern, selectOpts{requireToolCall: true})
	switch {
	case okMain && okCodex:
		if compareVersionSegments(codexLine.version, mainline.version) >= 0 {
			return codexLine.id
		}
		return mainline.id
	case okMain:
		return mainline.id
	case okCodex:
		return codexLine.id
	default:
		return FallbackCodexDefaultModel
	}
}

// ProbeModel 返回 Codex 健康与连通性探测模型。
func (p *DefaultModelPolicy) ProbeModel(platform string) string {
	if requireCodexPlatform(platform) != nil {
		return ""
	}
	// Probes must remain predictable across pricing/catalog synchronization.
	// A provider can still override this through AvailabilityConfig.TestModel.
	return FallbackCodexProbeModel
}

// ProbeCandidates 返回固定 Codex 探测模型。
func (p *DefaultModelPolicy) ProbeCandidates(platform string) []string {
	if requireCodexPlatform(platform) != nil {
		return nil
	}
	return []string{FallbackCodexProbeModel}
}

// DefaultModels 汇总当前解析结果。
func (p *DefaultModelPolicy) DefaultModels() DefaultModels {
	return DefaultModels{
		CodexDefault: p.CodexDefaultModel(),
		CodexProbe:   p.ProbeModel(CodexPlatform),
	}
}

// —— 家族匹配 ——

// familyPattern 从模型 id 提取版本段与频道信息;不匹配则该 id 不属于此家族。
type familyPattern func(id string) (version []int, dated string, preview bool, ok bool)

var (
	codexMainlineRe = regexp.MustCompile(`^gpt-(\d+(?:\.\d+)*)$`)
	codexLineRe     = regexp.MustCompile(`^gpt-(\d+(?:\.\d+)*)-codex$`)
)

func codexMainlinePattern(id string) ([]int, string, bool, bool) {
	m := codexMainlineRe.FindStringSubmatch(id)
	if m == nil {
		return nil, "", false, false
	}
	return parseVersionSegments(m[1]), "", false, true
}

func codexLinePattern(id string) ([]int, string, bool, bool) {
	m := codexLineRe.FindStringSubmatch(id)
	if m == nil {
		return nil, "", false, false
	}
	return parseVersionSegments(m[1]), "", false, true
}

// parseVersionSegments 把 "5.10"/"4-5" 拆成数字段。
func parseVersionSegments(s string) []int {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '-' })
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// compareVersionSegments 数字段逐段比较(5.10 > 5.9),缺段按 0。
func compareVersionSegments(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}

type selectOpts struct {
	requireToolCall bool
	stabilityWindow time.Duration
	preferDated     bool
}

type resolvedCandidate struct {
	id      string
	version []int
	dated   string
	preview bool
	release time.Time
}

// selectModel 在指定厂商目录内按家族规则选出最优候选。
// 过滤:文本入出、正价、release_date 合法且非未来、稳定窗、tool_call(按需)。
// 排序:版本降序 → stable 优先 → release 降序 → 带日期变体(按需) → id 升序。
func (p *DefaultModelPolicy) selectModel(providerID string, pattern familyPattern, opts selectOpts) (resolvedCandidate, bool) {
	catalog := p.catalog(providerID)
	if catalog == nil || len(catalog.Models) == 0 {
		return resolvedCandidate{}, false
	}
	now := time.Now()
	if p.now != nil {
		now = p.now()
	}

	candidates := make([]resolvedCandidate, 0, 8)
	for id := range catalog.Models {
		model := catalog.Models[id]
		version, dated, preview, ok := pattern(id)
		if !ok || len(version) == 0 {
			continue
		}
		if !model.IsTextModel() {
			continue
		}
		if opts.requireToolCall && !model.ToolCallAllowed() {
			continue
		}
		release, hasRelease := model.ReleaseTime()
		if !hasRelease {
			continue
		}
		if release.After(now.Add(resolverMaxFutureSkew)) {
			continue
		}
		if opts.stabilityWindow > 0 && now.Sub(release) < opts.stabilityWindow {
			continue
		}
		if p.pricing == nil || !p.pricing.HasPositivePricing(id) {
			continue
		}
		candidates = append(candidates, resolvedCandidate{
			id: id, version: version, dated: dated, preview: preview, release: release,
		})
	}
	if len(candidates) == 0 {
		return resolvedCandidate{}, false
	}

	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if cmp := compareVersionSegments(a.version, b.version); cmp != 0 {
			return cmp > 0
		}
		if a.preview != b.preview {
			return !a.preview // 同版本 stable 优先
		}
		if !a.release.Equal(b.release) {
			return a.release.After(b.release)
		}
		if opts.preferDated && (a.dated != "") != (b.dated != "") {
			return a.dated != ""
		}
		return a.id < b.id
	})
	return candidates[0], true
}
