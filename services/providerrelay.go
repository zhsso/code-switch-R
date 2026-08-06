package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	modelpricing "codeswitch/resources/model-pricing"

	"github.com/daodao97/xgo/xdb"
	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// warnedServiceTiers 去重容器:首次见到未知 service_tier 时告警,之后静默。
var warnedServiceTiers sync.Map

// warnUnknownTier 在首次遇到未知 service_tier 值时打印一次警告。
// 同值的后续请求静默,不同未知 tier 分别告警一次。
func warnUnknownTier(tier string) {
	if tier == "" {
		return
	}
	if _, loaded := warnedServiceTiers.LoadOrStore(tier, struct{}{}); loaded {
		return
	}
	fmt.Printf("⚠️  unknown service_tier=%q,保留原值入库,按 default 档计费\n", tier)
}

// LastUsedProvider 最后使用的供应商信息
// @author sm
type LastUsedProvider struct {
	Platform     string `json:"platform"`
	ProviderName string `json:"provider_name"` // 供应商名称
	UpdatedAt    int64  `json:"updated_at"`    // 更新时间（毫秒）
}

type ProviderRelayService struct {
	providerService     *ProviderService
	blacklistService    *BlacklistService
	dailyLimitService   *DailyCostLimitService
	notificationService *NotificationService
	requestEventService *RequestEventService
	appSettings         *AppSettingsService // 应用设置服务（用于获取轮询开关状态）
	server              *http.Server
	serverMu            sync.Mutex // 保护 server：Start/Stop 均可从前端 RPC 触发
	requestMu           sync.Mutex
	acceptingRequests   bool
	requestWG           sync.WaitGroup
	addr                string
	// boundAddrs 本次启动实际绑定成功的地址。监听地址在启动时就已冻结，
	// 之后改设置不会重绑，所以任何"这个地址能不能连"的判断都必须以它为准，
	// 不能拿磁盘上的设置去推断。
	boundAddrs  []string
	lastUsed    map[string]*LastUsedProvider // 各平台最后使用的供应商
	lastUsedMu  sync.RWMutex                 // 保护 lastUsed 的锁
	rrMu        sync.Mutex                   // 轮询状态锁
	rrLastStart map[string]string            // 轮询状态：key="platform:level" → value=上次起始 Provider Name
	// endpointCooldowns 多地址供应商的地址冷却状态（进程内，issue #27）
	endpointCooldowns *endpointCooldownStore
	// concurrency 按供应商并发配额（进程内，issue #21）
	concurrency *concurrencyLimiter
	// captureRequests 抓包模式开关（进程内状态，重启即关，issue #5）
	captureRequests atomic.Bool
	// captureClearGen "清除抓包数据"的代次。采集时记在 requestLog 上，落库前
	// 不一致即置空：清除动作之后才结束的在途长流请求，不得把已被用户删除的
	// 那批抓包内容重新写回
	captureClearGen atomic.Int64
	// captureWriteMu 让"清除/删除会话"与"落库提交"线性化：写侧以读锁包住
	// 代次校验 + INSERT 提交，清除以写锁包住代次推进 + UPDATE。
	// 消除"校验通过后、提交完成前恰好发生清除"的写回窗口
	captureWriteMu sync.RWMutex
	// captureSessionID 当前录制会话 id（0=无）。会话生命周期见 capturesession.go
	captureSessionID atomic.Int64
	// captureDeletedSessions 已删除会话的墓碑（captureWriteMu 保护）：
	// 在途长流请求落库时若其会话已被单独删除，捕获内容自我置空。
	// 会话只会由当前进程产生新行，进程生命期的墓碑即完备
	captureDeletedSessions map[int64]struct{}
	// captureRecoverOnce 每进程一次的遗留会话恢复（Start 可被前端重复触发）
	captureRecoverOnce sync.Once
	// captureInflightBytes 在途抓包缓冲的总占用（全量模式的内存兜底，见 captureBuffer）
	captureInflightBytes atomic.Int64
}

// errClientAbort 表示客户端中断连接，不应计入 provider 失败次数
var errClientAbort = errors.New("client aborted, skip failure count")

// errUpstreamStreamAborted 表示上游返回 2xx 后中途断流。
// 此时响应头与部分内容已经写给客户端，不能再降级到其它供应商（会写出两段响应），
// 但必须计入供应商失败，否则坏供应商永远不会被拉黑。
var errUpstreamStreamAborted = errors.New("upstream stream aborted after response started")

// errUpstreamClientError 表示上游以"请求本身有问题"为由拒绝（400/413/422 等）。
// 换供应商同样会失败，因此不计入供应商失败次数，避免一个坏请求把所有供应商拉黑。
var errUpstreamClientError = errors.New("upstream rejected the request payload")

// relayHTTPClient 转发共用的 HTTP 客户端。
// xrequest 的默认路径每次调用都会新建 http.Client 与 http.Transport，
// 连接完全无法复用，用后的空闲连接与其读写协程会长期滞留；这里改为共享连接池。
// 超时设为与原实现一致的 32 小时（适配超大型项目分析），实际的提前中止依靠
// 请求 context —— 客户端断开时立刻释放上游连接。
var relayHTTPClient = &http.Client{
	Timeout: 32 * time.Hour,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	},
}

// relayHTTPClientInsecure 与 relayHTTPClient 参数完全一致，仅跳过上游 TLS 证书验证，
// 供显式开启 insecureSkipVerify 的供应商使用（自签名证书/企业代理场景）。
// 独立实例：两种验证策略不能共用同一个 Transport 的连接池。
var relayHTTPClientInsecure = &http.Client{
	Timeout: 32 * time.Hour,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
	},
}

// warnedInsecureProviders 去重容器：开启跳验的供应商进程内首次使用时告警一次。
var warnedInsecureProviders sync.Map

// warnInsecureProviderOnce 首次对该供应商使用不验证 TLS 的客户端时打印警告，
// 作为跳验生效的审计痕迹。
func warnInsecureProviderOnce(name string) {
	if _, loaded := warnedInsecureProviders.LoadOrStore(name, struct{}{}); loaded {
		return
	}
	fmt.Printf("⚠️  Provider %s 已开启跳过 TLS 证书验证（insecureSkipVerify），存在中间人风险\n", name)
}

// relayClientFor 按供应商的 insecureSkipVerify 选择共享转发客户端。
// 返回的是共享实例，严禁在其上调 xrequest 的 SetTimeout（会写回 client，产生数据竞争）。
func relayClientFor(insecure bool, providerName string) *http.Client {
	if !insecure {
		return relayHTTPClient
	}
	warnInsecureProviderOnce(providerName)
	return relayHTTPClientInsecure
}

// deleteHeaderFold 按 HTTP 头大小写不敏感的语义删除。
// cloneHeaders 拿到的是 Go 规范化后的键名（如 X-Api-Key），
// 用小写字面量 delete 删不掉，必须逐个比对。
func deleteHeaderFold(headers map[string]string, names ...string) {
	for _, name := range names {
		for key := range headers {
			if strings.EqualFold(key, name) {
				delete(headers, key)
			}
		}
	}
}

// getHeaderFold 大小写不敏感地取头部值。
func getHeaderFold(headers map[string]string, name string) string {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

// setHeaderCanonical 以规范化键名写入头部，并先清掉同名的其它大小写形式。
// xrequest 是 req.Header[k] = []string{v} 直接赋值不做规范化，
// 若不先清理，客户端的 X-Api-Key 与注入的 x-api-key 会作为两个条目同时发到上游。
func setHeaderCanonical(headers map[string]string, name string, value string) {
	deleteHeaderFold(headers, name)
	headers[http.CanonicalHeaderKey(name)] = value
}

var clientCredentialHeaders = []string{
	"authorization", "proxy-authorization", "x-api-key", "api-key", "x-goog-api-key",
}

// sanitizeUpstreamHeaders 清理透传给上游的客户端请求头。
//
// 必须在注入供应商凭据之前调用：它会按大小写不敏感的方式删掉所有认证类头，
// 放到注入之后调用会把刚写入的供应商凭据一并删除。
//
// 具体清理三类：
//  1. 认证类头必须清空，否则用户本机的真实 API Key 会随请求一起发给每个第三方供应商；
//  2. Accept-Encoding 必须交回 Go 协商，透传客户端的值会让 Go 不再自动解压，
//     SSE 解析与 usage 提取拿到的是压缩字节，计费恒为 0；
//  3. 逐跳头（Connection/TE 等）不应跨代理转发。
func sanitizeUpstreamHeaders(headers map[string]string) {
	deleteHeaderFold(headers, clientCredentialHeaders...)
	deleteHeaderFold(headers,
		"accept-encoding", "connection", "keep-alive", "transfer-encoding", "te", "upgrade")
}

// credentialQueryParams 查询串中承载凭据的参数名（小写比较）。
var credentialQueryParams = map[string]bool{
	"key":          true,
	"api_key":      true,
	"apikey":       true,
	"access_token": true,
	"token":        true,
}

// stripCredentialQueryParams 从原始查询串里删掉凭据类参数，其余参数保持原样与原顺序
// （alt=sse 这类必须保留，且不能因重新编码改变值）。
func stripCredentialQueryParams(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	parts := strings.Split(rawQuery, "&")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		name := part
		if eq := strings.Index(part, "="); eq >= 0 {
			name = part[:eq]
		}
		if credentialQueryParams[strings.ToLower(name)] {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "&")
}

// maskSensitiveQuery 把 URL 查询串里的凭据类参数替换掉再进日志。
func maskSensitiveQuery(rawURL string) string {
	qIdx := strings.Index(rawURL, "?")
	if qIdx < 0 {
		return rawURL
	}
	path, rawQuery := rawURL[:qIdx], rawURL[qIdx+1:]
	parts := strings.Split(rawQuery, "&")
	for i, part := range parts {
		eq := strings.Index(part, "=")
		if eq <= 0 {
			continue
		}
		if credentialQueryParams[strings.ToLower(part[:eq])] {
			parts[i] = part[:eq] + "=***"
		}
	}
	return path + "?" + strings.Join(parts, "&")
}

// checkNonStreamTruncated 校验非流式响应是否被上游截断。
//
// xrequest 读取非流式响应体时丢弃了 io.ReadAll 的错误（xrequest/response.go 的
// `body, _ := io.ReadAll(...)`），上游中途断连会被当作完整响应，
// 于是半死的供应商在非流式请求上永远被判成功、失败计数被清零、永远不会被拉黑。
// 上游声明了 Content-Length 时可以用它与实际写给客户端的字节数比对；
// 分块传输（无 Content-Length）或内容被压缩时无从校验，返回 nil 维持原行为。
func checkNonStreamTruncated(resp *xrequest.Response, written int64) error {
	if resp == nil || resp.RawResponse == nil {
		return nil
	}
	declared := resp.RawResponse.ContentLength
	if declared <= 0 {
		return nil // 分块传输、未声明长度或空响应体
	}
	// 上游若做了内容压缩，解压后长度与 Content-Length 不可比
	if resp.RawResponse.Header.Get("Content-Encoding") != "" {
		return nil
	}
	if written < declared {
		return fmt.Errorf("响应被截断: 实际 %d 字节，上游声明 %d 字节", written, declared)
	}
	return nil
}

// respondNoEligibleProviders 初筛后无可用供应商的 404 终态。
// 把"为什么被跳过"按原因拆开讲清并给排查指引：白名单不匹配、临时拉黑与
// 未启用是三种完全不同的处置方式，混在一个计数里用户无从下手（issue #29）。
// 多种原因并存时全部列出，不做"选一个当代表"的省略
func respondNoEligibleProviders(c *gin.Context, requestedModel string, skippedModel, skippedBlacklist, skippedInvalid int, dailySkipped ...int) {
	skippedDaily := 0
	if len(dailySkipped) > 0 {
		skippedDaily = dailySkipped[0]
	}
	var reasons, hints []string
	if skippedModel > 0 {
		reasons = append(reasons, fmt.Sprintf("%d 个供应商的模型白名单/映射不包含该模型", skippedModel))
		hints = append(hints, "在主页打开对应供应商，确认\"支持的模型\"包含该模型或留空（留空=支持所有模型），\"模型映射\"的目标模型名必须在白名单内")
	}
	if skippedBlacklist > 0 {
		reasons = append(reasons, fmt.Sprintf("%d 个正被临时拉黑", skippedBlacklist))
		hints = append(hints, "被拉黑的供应商可等待自动恢复，或到黑名单页手动解除")
	}
	if skippedDaily > 0 {
		reasons = append(reasons, fmt.Sprintf("%d 个已达到当日费用限额", skippedDaily))
		hints = append(hints, "可在 Provider 的额度管理中查看用量，次日自动恢复或临时解禁")
	}
	if skippedInvalid > 0 {
		reasons = append(reasons, fmt.Sprintf("%d 个配置校验失败（详见控制台日志）", skippedInvalid))
		hints = append(hints, "配置校验失败的常见原因是模型映射的目标不在白名单内")
	}

	var msg string
	if len(reasons) == 0 {
		msg = "当前平台没有已启用的供应商。请在主页添加供应商并确认其已启用、API 地址与密钥已填写"
	} else {
		head := "没有可用的供应商"
		if requestedModel != "" {
			head = fmt.Sprintf("没有可用的供应商支持模型 '%s'", requestedModel)
		}
		msg = fmt.Sprintf("%s：%s。排查：%s", head, strings.Join(reasons, "；"), strings.Join(hints, "；"))
	}
	c.JSON(http.StatusNotFound, gin.H{"error": msg})
}

// respondAllProvidersFailed 统一输出"所有供应商都失败"的终态响应。
//
// 只有当**每一次**失败都是上游判定请求内容有问题（400/413/422 等）时才回 4xx：
// 这类请求换供应商也不可能成功，回 502 会让 SDK 按服务端故障自动重试，
// 一个坏请求被反复重发，每次都完整扫一遍全部供应商、白耗上游配额。
//
// 反之只要有任何一次是真的供应商故障（超时、5xx、限流），就必须维持 502 让 SDK 退避重试——
// 只看最后一个错误会误判：降级链末尾往往是最挑剔的备用供应商，
// 它回的 400 会把前面那个"临时过载、稍后可用"的供应商掩盖掉。
func respondAllProvidersFailed(c *gin.Context, lastError error, allClientErrors bool, payload gin.H) {
	status := http.StatusBadGateway
	if allClientErrors && errors.Is(lastError, errUpstreamClientError) {
		status = http.StatusBadRequest
		payload["type"] = "error"
		payload["error"] = map[string]string{
			"type":    "invalid_request_error",
			"message": lastError.Error(),
		}
	}
	c.JSON(status, payload)
}

// respondAllBusy 纯并发满载终态：503 + Retry-After，带稳定机器码
// provider_concurrency_exhausted。Codex 的 Responses API 使用 OpenAI 错误结构；
// 不用 502（那表示已联系上游失败）也不用 504（不是上游超时）。
func respondAllBusy(c *gin.Context, kind string) {
	c.Header("Retry-After", "1")
	msg := "所有可用供应商均已达到并发上限，请稍后重试"
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error": gin.H{
			"type":    "server_error",
			"code":    "provider_concurrency_exhausted",
			"message": msg,
		},
	})
}

// isClientSideUpstreamStatus 判定上游 4xx 是否属于"请求内容本身有问题"。
// 这类失败换供应商也一样，不应计入供应商失败次数；
// 401/403/404/408/429 仍属供应商侧问题（密钥失效、路径配错、限流），保持计入。
func isClientSideUpstreamStatus(status int) bool {
	switch status {
	case http.StatusBadRequest,
		http.StatusRequestEntityTooLarge,
		http.StatusUnsupportedMediaType,
		http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// NewProviderRelayService 构造代理服务。
func NewProviderRelayService(providerService *ProviderService, blacklistService *BlacklistService, notificationService *NotificationService, appSettings *AppSettingsService, addr string, eventServices ...*RequestEventService) *ProviderRelayService {
	if addr == "" {
		addr = "127.0.0.1:18100" // 【安全修复】仅监听本地回环地址，防止 API Key 暴露到局域网
	}

	var requestEventService *RequestEventService
	if len(eventServices) > 0 {
		requestEventService = eventServices[0]
	}

	return &ProviderRelayService{
		providerService:     providerService,
		blacklistService:    blacklistService,
		notificationService: notificationService,
		requestEventService: requestEventService,
		appSettings:         appSettings,
		acceptingRequests:   true,
		addr:                addr,
		lastUsed: map[string]*LastUsedProvider{
			CodexPlatform: nil,
		},
		rrLastStart:            make(map[string]string),
		endpointCooldowns:      newEndpointCooldownStore(),
		concurrency:            newConcurrencyLimiter(),
		captureDeletedSessions: make(map[int64]struct{}),
	}
}

func (prs *ProviderRelayService) SetRequestEventService(service *RequestEventService) {
	prs.requestEventService = service
}

func (prs *ProviderRelayService) SetDailyCostLimitService(service *DailyCostLimitService) {
	prs.dailyLimitService = service
}

func (prs *ProviderRelayService) isDailyCostBlocked(kind string, provider Provider) bool {
	if prs.dailyLimitService == nil {
		return false
	}
	blocked, err := prs.dailyLimitService.IsProviderBlocked(kind, provider)
	if err != nil {
		fmt.Printf("[WARN] Provider %s 每日额度状态读取失败: %v\n", provider.Name, err)
		// An enabled limit fails closed so a storage/configuration error cannot
		// silently turn an enforced budget into unlimited routing.
		return provider.DailyCostLimitEnabled
	}
	return blocked
}

// setLastUsedProvider 记录最后使用的供应商
// @author sm
func (prs *ProviderRelayService) setLastUsedProvider(platform, providerName string) {
	if requireCodexPlatform(platform) != nil {
		return
	}
	prs.lastUsedMu.Lock()
	defer prs.lastUsedMu.Unlock()
	prs.lastUsed[CodexPlatform] = &LastUsedProvider{
		Platform:     CodexPlatform,
		ProviderName: providerName,
		UpdatedAt:    time.Now().UnixMilli(),
	}
}

// GetLastUsedProvider 获取指定平台最后使用的供应商
// @author sm
func (prs *ProviderRelayService) GetLastUsedProvider(platform string) *LastUsedProvider {
	if requireCodexPlatform(platform) != nil {
		return nil
	}
	prs.lastUsedMu.RLock()
	defer prs.lastUsedMu.RUnlock()
	return prs.lastUsed[CodexPlatform]
}

// GetAllLastUsedProviders 获取所有平台最后使用的供应商
// @author sm
func (prs *ProviderRelayService) GetAllLastUsedProviders() map[string]*LastUsedProvider {
	prs.lastUsedMu.RLock()
	defer prs.lastUsedMu.RUnlock()
	return map[string]*LastUsedProvider{CodexPlatform: prs.lastUsed[CodexPlatform]}
}

// isRoundRobinSettingEnabled 检查轮询设置是否启用（纯读取 AppSettings，不受 Fixed Mode 影响）
// 用于在 Fixed Mode 分支内也支持轮询排序
func (prs *ProviderRelayService) isRoundRobinSettingEnabled() bool {
	if prs.appSettings == nil {
		return false
	}
	settings, err := prs.appSettings.GetAppSettings()
	if err != nil {
		return false
	}
	return settings.EnableRoundRobin
}

// isRoundRobinEnabled 检查轮询功能是否启用（仅在降级模式下使用）
// 条件：1. 应用设置开关启用 2. 拉黑模式关闭（Fixed Mode 走单独分支处理轮询）
func (prs *ProviderRelayService) isRoundRobinEnabled() bool {
	// Fixed Mode 分支内有独立的轮询处理逻辑，此处返回 false 走降级模式
	if prs.blacklistService.ShouldUseFixedMode() {
		return false
	}
	return prs.isRoundRobinSettingEnabled()
}

// roundRobinOrder 对同 Level 的 providers 进行轮询排序
// 算法：基于 name 追踪，将上次起始 provider 移到末尾，实现轮询效果
// 参数：
//   - platform: 平台标识
//   - level: 当前 Level
//   - providers: 同 Level 的 providers 列表（已过滤、按用户排序）
//
// 返回：轮询排序后的 providers 列表（新切片，不修改原切片）
func (prs *ProviderRelayService) roundRobinOrder(platform string, level int, providers []Provider) []Provider {
	if len(providers) <= 1 {
		return providers
	}

	// 构建 key: "platform:level"
	key := fmt.Sprintf("%s:%d", platform, level)

	prs.rrMu.Lock()
	defer prs.rrMu.Unlock()

	lastStart := prs.rrLastStart[key]

	// 记录本次起始 provider 名称（更新状态）
	prs.rrLastStart[key] = providers[0].Name

	// 如果没有历史记录，返回原顺序
	if lastStart == "" {
		return providers
	}

	// 查找上次起始 provider 在当前列表中的位置
	lastIdx := -1
	for i, p := range providers {
		if p.Name == lastStart {
			lastIdx = i
			break
		}
	}

	// 上次起始 provider 不在当前列表（可能被禁用/黑名单），返回原顺序
	if lastIdx == -1 {
		return providers
	}

	// 构建轮询顺序：从 lastIdx+1 开始，环形遍历
	result := make([]Provider, len(providers))
	for i := 0; i < len(providers); i++ {
		idx := (lastIdx + 1 + i) % len(providers)
		result[i] = providers[idx]
	}

	// 更新本次起始 provider 名称
	prs.rrLastStart[key] = result[0].Name

	return result
}

func (prs *ProviderRelayService) Start() error {
	// Repeated Start calls must not replace prs.server; otherwise the old
	// listener and Serve goroutine can no longer be stopped.
	prs.serverMu.Lock()
	alreadyRunning := prs.server != nil
	prs.serverMu.Unlock()
	if alreadyRunning {
		return fmt.Errorf("provider relay 已在 %s 上运行", prs.addr)
	}

	// 启动前验证配置
	if warnings := prs.validateConfig(); len(warnings) > 0 {
		fmt.Println("======== Provider 配置验证警告 ========")
		for _, warn := range warnings {
			fmt.Printf("⚠️  %s\n", warn)
		}
		fmt.Println("========================================")
	}

	router := gin.Default()
	prs.registerRoutes(router)

	listener, err := net.Listen("tcp", prs.addr)
	if err != nil {
		return fmt.Errorf("listen provider relay on %s: %w", prs.addr, err)
	}

	server := &http.Server{
		Addr:    prs.addr,
		Handler: router,
	}
	prs.serverMu.Lock()
	prs.server = server
	prs.serverMu.Unlock()
	prs.requestMu.Lock()
	prs.acceptingRequests = true
	prs.requestMu.Unlock()

	fmt.Printf("provider relay server listening on %s\n", listener.Addr().String())
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Printf("provider relay server error: %v\n", err)
		}
	}()

	prs.serverMu.Lock()
	prs.boundAddrs = []string{listener.Addr().String()}
	prs.serverMu.Unlock()
	return nil
}

// BoundAddresses 返回本次启动实际绑定成功的监听地址。
// 监听地址在启动时冻结，改了网络设置也要重启应用才生效，
// 所以 UI 展示必须以此为准。
func (prs *ProviderRelayService) BoundAddresses() []string {
	prs.serverMu.Lock()
	defer prs.serverMu.Unlock()
	return append([]string(nil), prs.boundAddrs...)
}

// GetRequestCapture 读取抓包模式开关
func (prs *ProviderRelayService) GetRequestCapture() bool {
	return prs.captureRequests.Load()
}

// buildCaptureURL 拼接抓包 URL：目标地址 + 查询参数（不脱敏，全量记录）
func buildCaptureURL(targetURL string, query map[string]string) string {
	if len(query) == 0 {
		return targetURL
	}
	values := url.Values{}
	for k, v := range query {
		values.Set(k, v)
	}
	sep := "?"
	if strings.Contains(targetURL, "?") {
		sep = "&"
	}
	return targetURL + sep + values.Encode()
}

// captureErrorResponse 已并入 extractUpstreamError（读错误体时一并入抓包缓冲），
// 此处不再单独提供。

type RequestLog struct {
	ID              int64   `json:"id"`
	Platform        string  `json:"platform"`
	Model           string  `json:"model"`
	Provider        string  `json:"provider"`
	HttpCode        int     `json:"http_code"`
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	CacheReadTokens int     `json:"cache_read_tokens"`
	ReasoningTokens int     `json:"reasoning_tokens"`
	IsStream        bool    `json:"is_stream"`
	DurationSec     float64 `json:"duration_sec"`
	CreatedAt       string  `json:"created_at"`
	ServiceTier     string  `json:"service_tier"`
	InputCost       float64 `json:"input_cost"`
	OutputCost      float64 `json:"output_cost"`
	ReasoningCost   float64 `json:"reasoning_cost"`
	CacheReadCost   float64 `json:"cache_read_cost"`
	TotalCost       float64 `json:"total_cost"`
	HasPricing      bool    `json:"has_pricing"`
	HasCapture      bool    `json:"has_capture"`

	RequestURL       string `json:"-"`
	RequestHeaders   string `json:"-"`
	RequestBody      string `json:"-"`
	BodyTruncated    bool   `json:"-"`
	BodyBytes        int    `json:"-"`
	ResponseHeaders  string `json:"-"`
	ResponseBody     string `json:"-"`
	RespTruncated    bool   `json:"-"`
	RespBytes        int    `json:"-"`
	BudgetSkipped    bool   `json:"-"`
	CaptureSessionID int64  `json:"-"`
	captureGen       int64
	respBuf          *captureBuffer
}

// finalizeCaptureResponse 把响应缓冲收敛进 requestLog 的响应字段（落库前调用一次）
func finalizeCaptureResponse(requestLog *RequestLog) {
	cb := requestLog.respBuf
	if cb == nil {
		return
	}
	requestLog.ResponseBody = string(cb.buf)
	requestLog.RespTruncated = cb.truncated
	requestLog.RespBytes = cb.total
	requestLog.BudgetSkipped = cb.budgetSkipped
}

// stripStaleCapture 落库前的校验：采集发生在请求开始，长流请求可能在
// "清空全部"（代次不一致）或"删除所属会话"（墓碑命中）之后才结束，
// 两种情况都说明这批内容已被用户要求删除，置空且摘除会话关联。
// 采集快照化后合法捕获行必有非零会话 id，携带内容却无会话的行只能是
// 竞态残迹，一并置空（否则会混进 0 号旧数据桶）。
// 调用方需持有 captureWriteMu 读锁（墓碑 map 由该锁保护）
func (prs *ProviderRelayService) stripStaleCapture(requestLog *RequestLog) {
	_, deleted := prs.captureDeletedSessions[requestLog.CaptureSessionID]
	orphan := requestLog.CaptureSessionID == 0 && requestLogHasCapture(requestLog)
	if requestLog.captureGen != prs.captureClearGen.Load() || deleted || orphan {
		requestLog.RequestURL = ""
		requestLog.RequestHeaders = ""
		requestLog.RequestBody = ""
		requestLog.BodyTruncated = false
		requestLog.BodyBytes = 0
		requestLog.ResponseHeaders = ""
		requestLog.ResponseBody = ""
		requestLog.RespTruncated = false
		requestLog.RespBytes = 0
		requestLog.BudgetSkipped = false
		requestLog.CaptureSessionID = 0
	}
}

// requestLogInsertSQL 两条写入路径共用的列清单，避免写入结构分叉。
const requestLogInsertSQL = `
	INSERT INTO request_log (
		platform, model, provider, http_code,
		input_tokens, output_tokens, cache_read_tokens,
		reasoning_tokens, is_stream, duration_sec,
		service_tier,
		request_url, request_headers, request_body, body_truncated, body_bytes,
		response_headers, response_body, response_truncated, response_bytes, budget_skipped,
		capture_session_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

func requestLogInsertArgs(requestLog *RequestLog) []interface{} {
	return []interface{}{
		requestLog.Platform, requestLog.Model, requestLog.Provider, requestLog.HttpCode,
		requestLog.InputTokens, requestLog.OutputTokens, requestLog.CacheReadTokens,
		requestLog.ReasoningTokens,
		boolToInt(requestLog.IsStream), requestLog.DurationSec,
		requestLog.ServiceTier,
		requestLog.RequestURL, requestLog.RequestHeaders, requestLog.RequestBody,
		boolToInt(requestLog.BodyTruncated), requestLog.BodyBytes,
		requestLog.ResponseHeaders, requestLog.ResponseBody,
		boolToInt(requestLog.RespTruncated), requestLog.RespBytes, boolToInt(requestLog.BudgetSkipped),
		requestLog.CaptureSessionID,
	}
}

// requestLogHasCapture 判断该行是否携带抓包 payload。会话标记行（session_id!=0）
// 也走同步栅栏写，避免与清除/删除竞态
func requestLogHasCapture(requestLog *RequestLog) bool {
	return requestLog.CaptureSessionID != 0 ||
		requestLog.RequestURL != "" || requestLog.RequestHeaders != "" || requestLog.RequestBody != "" ||
		requestLog.ResponseHeaders != "" || requestLog.ResponseBody != "" ||
		requestLog.BodyTruncated || requestLog.BodyBytes != 0 ||
		requestLog.RespTruncated || requestLog.RespBytes != 0 || requestLog.BudgetSkipped
}

// writeRequestLog 落库统一入口，调用方需已持有 captureWriteMu 读锁。
// 携带抓包内容的行同步直写——提交在读锁内完成，与清除的写锁真正线性化
// （批量队列的 ExecBatchCtx 超时后任务仍会执行，"返回"不等于"已提交"，
// 不能作为栅栏边界）；普通行保持批量队列路径，不受清除语义约束。
// 抓包是低频调试态，直写不构成写入热点
func (prs *ProviderRelayService) writeRequestLog(requestLog *RequestLog) error {
	if requestLogHasCapture(requestLog) {
		db, err := xdb.DB("default")
		if err != nil {
			return err
		}
		_, err = db.Exec(requestLogInsertSQL, requestLogInsertArgs(requestLog)...)
		return err
	}
	if GlobalDBQueueLogs == nil {
		return fmt.Errorf("队列未初始化")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return GlobalDBQueueLogs.ExecBatchCtx(ctx, requestLogInsertSQL, requestLogInsertArgs(requestLog)...)
}

// validateConfig 验证所有 provider 的配置
// 返回警告列表（非阻塞性错误）
func (prs *ProviderRelayService) validateConfig() []string {
	warnings := make([]string, 0)

	for _, kind := range []string{CodexPlatform} {
		providers, err := prs.providerService.LoadProviders(kind)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("[%s] 加载配置失败: %v", kind, err))
			continue
		}

		enabledCount := 0
		for _, p := range providers {
			if !p.Enabled {
				continue
			}
			enabledCount++

			// 验证每个启用的 provider
			if errs := p.ValidateConfiguration(); len(errs) > 0 {
				for _, errMsg := range errs {
					warnings = append(warnings, fmt.Sprintf("[%s/%s] %s", kind, p.Name, errMsg))
				}
			}

			// 检查是否配置了模型白名单或映射
			if (p.SupportedModels == nil || len(p.SupportedModels) == 0) &&
				(p.ModelMapping == nil || len(p.ModelMapping) == 0) {
				warnings = append(warnings, fmt.Sprintf(
					"[%s/%s] 未配置 supportedModels 或 modelMapping，将假设支持所有模型（可能导致降级失败）",
					kind, p.Name))
			}

			// 检查是否只配置了映射但没有白名单
			if len(p.ModelMapping) > 0 && len(p.SupportedModels) == 0 {
				warnings = append(warnings, fmt.Sprintf(
					"[%s/%s] 配置了 modelMapping 但未配置 supportedModels，映射目标将不做校验，请确认目标模型在供应商处可用",
					kind, p.Name))
			}
		}

		if enabledCount == 0 {
			warnings = append(warnings, fmt.Sprintf("[%s] 没有启用的 provider", kind))
		}
	}

	return warnings
}

func (prs *ProviderRelayService) Stop() error {
	prs.requestMu.Lock()
	prs.acceptingRequests = false
	prs.requestMu.Unlock()

	prs.serverMu.Lock()
	server := prs.server
	prs.server = nil
	// 清掉绑定地址：停掉之后再对外报告"正在监听 xxx"会误导 UI
	prs.boundAddrs = nil
	prs.serverMu.Unlock()

	if server == nil {
		// 代理本就未运行；若录制开关还开着（异常路径），同样封存会话
		prs.closeActiveCaptureSession()
		return prs.waitForActiveRequests(5 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := server.Shutdown(ctx)
	if err != nil {
		// 优雅关闭超时（长流式请求会一直占着连接）：强制关闭监听与全部连接，
		// 否则 Serve 协程与在途连接会活过 OnShutdown
		fmt.Printf("[WARN] 代理优雅关闭超时，强制关闭: %v\n", err)
		if closeErr := server.Close(); closeErr != nil {
			fmt.Printf("[WARN] 强制关闭代理失败: %v\n", closeErr)
		}
	}
	drainErr := prs.waitForActiveRequests(5 * time.Second)
	// 代理停了就不再有新流量，正常封存录制中的会话（区别于崩溃后的"已中断"）
	prs.closeActiveCaptureSession()
	return errors.Join(err, drainErr)
}

func (prs *ProviderRelayService) waitForActiveRequests(timeout time.Duration) error {
	done := make(chan struct{})
	go func() {
		prs.requestWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting %v for active relay requests", timeout)
	}
}

func (prs *ProviderRelayService) trackRequest(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		prs.requestMu.Lock()
		if !prs.acceptingRequests {
			prs.requestMu.Unlock()
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "provider relay is shutting down"})
			return
		}
		prs.requestWG.Add(1)
		prs.requestMu.Unlock()
		defer prs.requestWG.Done()
		handler(c)
	}
}

func (prs *ProviderRelayService) Addr() string {
	return prs.addr
}

func (prs *ProviderRelayService) registerRoutes(router gin.IRouter) {
	router.POST("/responses", prs.trackRequest(prs.proxyHandler(CodexPlatform, "/responses")))
	router.GET("/v1/models", prs.trackRequest(prs.modelsHandler(CodexPlatform)))
}

func (prs *ProviderRelayService) proxyHandler(kind string, endpoint string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := requireCodexPlatform(kind); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		trace := newRelayRequestTrace(prs.requestEventService, kind)
		c.Header("X-Request-ID", trace.RequestID())
		defer func() {
			trace.Finish(c.Writer.Status(), c.Request.Context().Err() != nil)
		}()
		var bodyBytes []byte
		if c.Request.Body != nil {
			data, err := io.ReadAll(c.Request.Body)
			if err != nil {
				trace.RecordSummary("invalid_request", "request body could not be read")
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
				return
			}
			bodyBytes = data
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// 空 body 或非法 JSON 一定会被所有上游拒绝，提前挡掉：
		// 否则每个供应商都要挨一次 4xx，还会白耗一轮降级
		if !gjson.ValidBytes(bodyBytes) {
			trace.RecordSummary("invalid_request", "request body must be valid JSON")
			c.JSON(http.StatusBadRequest, gin.H{
				"type":    "error",
				"error":   map[string]string{"type": "invalid_request_error", "message": "request body must be valid JSON"},
				"message": "request body must be valid JSON",
			})
			return
		}

		isStream := gjson.GetBytes(bodyBytes, "stream").Bool()
		requestedModel := gjson.GetBytes(bodyBytes, "model").String()
		trace.SetModel(requestedModel)

		// 如果未指定模型，记录警告但不拦截
		if requestedModel == "" {
			fmt.Printf("[WARN] 请求未指定模型名，无法执行模型智能降级\n")
		}

		// (providers, 配置代数) 配对装载：容量热更新以更高代数为准，
		// 分两步读取会让旧配置带上新代数、降容被来回覆盖
		providers, configGen, err := prs.providerService.LoadProvidersWithGen(kind)
		if err != nil {
			trace.RecordSummary("relay_error", "provider configuration could not be loaded")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load providers"})
			return
		}

		active := make([]Provider, 0, len(providers))
		skippedCount := 0
		skippedModel, skippedBlacklist, skippedInvalid, skippedDaily := 0, 0, 0, 0
		for _, provider := range providers {
			// 基础过滤：enabled、URL、APIKey
			if !provider.Enabled || provider.APIURL == "" || provider.APIKey == "" {
				continue
			}

			// 配置验证：失败则自动跳过
			if errs := provider.ValidateConfiguration(); len(errs) > 0 {
				fmt.Printf("[WARN] Provider %s 配置验证失败，已自动跳过: %v\n", provider.Name, errs)
				skippedCount++
				skippedInvalid++
				continue
			}

			// 核心过滤：只保留支持请求模型的 provider
			if requestedModel != "" && !provider.IsModelSupported(requestedModel) {
				fmt.Printf("[INFO] Provider %s 不支持模型 %s，已跳过\n", provider.Name, requestedModel)
				skippedCount++
				skippedModel++
				continue
			}

			// 黑名单检查：跳过已拉黑的 provider
			if isBlacklisted, until := prs.blacklistService.IsBlacklisted(kind, provider.Name); isBlacklisted {
				fmt.Printf("⛔ Provider %s 已拉黑，过期时间: %v\n", provider.Name, until.Format("15:04:05"))
				skippedCount++
				skippedBlacklist++
				continue
			}
			if prs.isDailyCostBlocked(kind, provider) {
				fmt.Printf("⛔ Provider %s 已达到当日费用限额，跳过路由\n", provider.Name)
				skippedCount++
				skippedDaily++
				continue
			}

			active = append(active, provider)
		}

		if len(active) == 0 {
			trace.RecordSummary("no_eligible_provider", "no eligible provider was available")
			respondNoEligibleProviders(c, requestedModel, skippedModel, skippedBlacklist, skippedInvalid, skippedDaily)
			return
		}

		fmt.Printf("[INFO] 找到 %d 个可用的 provider（已过滤 %d 个）：", len(active), skippedCount)
		for _, p := range active {
			fmt.Printf("%s ", p.Name)
		}
		fmt.Println()

		// 按 Level 分组
		levelGroups := make(map[int][]Provider)
		for _, provider := range active {
			level := provider.Level
			if level <= 0 {
				level = 1 // 未配置或零值时默认为 Level 1
			}
			levelGroups[level] = append(levelGroups[level], provider)
		}

		// 获取所有 level 并升序排序
		levels := make([]int, 0, len(levelGroups))
		for level := range levelGroups {
			levels = append(levels, level)
		}
		sort.Ints(levels)

		fmt.Printf("[INFO] 共 %d 个 Level 分组：%v\n", len(levels), levels)

		query := flattenQuery(c.Request.URL.Query())
		clientHeaders := cloneHeaders(c.Request.Header)

		// 获取拉黑功能开关状态
		blacklistEnabled := prs.blacklistService.ShouldUseFixedMode()

		// 【拉黑模式】：同 Provider 重试直到被拉黑，然后切换到下一个 Provider
		// 单次客户端请求的重试次数可能低于拉黑阈值，
		// 通过内部重试机制，在单次请求中累积足够失败次数触发拉黑
		if blacklistEnabled {
			// 缓存轮询设置（单次请求级别，避免重复读取配置文件）
			roundRobinSettingEnabled := prs.isRoundRobinSettingEnabled()
			if roundRobinSettingEnabled {
				fmt.Printf("[INFO] 🔒 拉黑模式 + 轮询负载均衡\n")
			} else {
				fmt.Printf("[INFO] 🔒 拉黑模式（顺序调度）\n")
			}

			// 获取重试配置
			retryConfig := prs.blacklistService.GetRetryConfig()
			maxRetryPerProvider := retryConfig.FailureThreshold
			retryWaitSeconds := retryConfig.RetryWaitSeconds
			fmt.Printf("[INFO] 重试配置: 每 Provider 最多 %d 次重试，间隔 %d 秒\n",
				maxRetryPerProvider, retryWaitSeconds)

			var lastError error
			// 只要有过一次真正的供应商故障，终态就必须维持 502 让 SDK 退避重试
			sawNonClientError := false
			var lastProvider string
			totalAttempts := 0

			busyWaitDeadline := time.Time{}
			enteredBusyWait := false
			defer func() {
				if enteredBusyWait {
					prs.concurrency.leaveWaitPhase()
				}
			}()
			busySkipped := 0
			// 已实际尝试过的供应商：等待阶段重扫不再碰它（失败已计、重试预算不重置）
			attemptedProviders := map[string]bool{}
			// 因并发满被跳过、尚未真实尝试的候选
			busyPending := map[string]concurrencyBusyRef{}
			for {
				busySkipped = 0
				// 每 pass 重建：上一轮候选可能已被拉黑或删除，残留会让容量门控恒真
				busyPending = map[string]concurrencyBusyRef{}
				// 遍历所有 Level 和 Provider
				for _, level := range levels {
					providersInLevel := levelGroups[level]

					// 如果启用轮询，对同 Level 的 providers 进行轮询排序
					if roundRobinSettingEnabled {
						providersInLevel = prs.roundRobinOrder(kind, level, providersInLevel)
					}

					fmt.Printf("[INFO] === 尝试 Level %d（%d 个 provider）===\n", level, len(providersInLevel))

					for _, provider := range providersInLevel {
						if attemptedProviders[strconv.FormatInt(provider.ID, 10)] {
							continue
						}
						// 检查是否已被拉黑（跳过已拉黑的 provider）
						if blacklisted, until := prs.blacklistService.IsBlacklisted(kind, provider.Name); blacklisted {
							fmt.Printf("[INFO] ⏭️ 跳过已拉黑的 Provider: %s (解禁时间: %v)\n", provider.Name, until)
							continue
						}
						if prs.isDailyCostBlocked(kind, provider) {
							fmt.Printf("[INFO] ⏭️ 跳过当日额度耗尽的 Provider: %s\n", provider.Name)
							continue
						}

						// 获取实际模型名
						effectiveModel := provider.GetEffectiveModel(requestedModel)
						currentBodyBytes := bodyBytes
						if effectiveModel != requestedModel && requestedModel != "" {
							fmt.Printf("[INFO] Provider %s 映射模型: %s -> %s\n", provider.Name, requestedModel, effectiveModel)
							modifiedBody, err := ReplaceModelInRequestBody(bodyBytes, effectiveModel)
							if err != nil {
								trace.RecordLocalSummary(provider.Name, "model_mapping_error", "provider model mapping could not be applied")
								fmt.Printf("[ERROR] 模型映射失败: %v，跳过此 Provider\n", err)
								continue
							}
							currentBodyBytes = modifiedBody
						}

						// 获取有效端点
						effectiveEndpoint := provider.GetEffectiveEndpoint(endpoint)

						// 同 Provider 内重试循环
						for retryCount := 0; retryCount < maxRetryPerProvider; retryCount++ {
							totalAttempts++

							// 再次检查是否已被拉黑（重试过程中可能被拉黑）
							if blacklisted, _ := prs.blacklistService.IsBlacklisted(kind, provider.Name); blacklisted {
								fmt.Printf("[INFO] 🚫 Provider %s 已被拉黑，切换到下一个\n", provider.Name)
								break
							}
							if prs.isDailyCostBlocked(kind, provider) {
								fmt.Printf("[INFO] 🚫 Provider %s 已达到当日费用限额，切换到下一个\n", provider.Name)
								break
							}

							fmt.Printf("[INFO] [拉黑模式] Provider: %s (Level %d) | 重试 %d/%d | Model: %s\n",
								provider.Name, level, retryCount+1, maxRetryPerProvider, effectiveModel)

							attempt := trace.BeforeAttempt(provider.Name)
							startTime := time.Now()
							ok, err := prs.forwardRequest(c, kind, provider, effectiveEndpoint, query, clientHeaders, currentBodyBytes, isStream, effectiveModel, configGen)
							duration := time.Since(startTime)
							if err != nil {
								trace.RecordForwardError(provider.Name, err, attempt, retryCount+1, duration)
							}

							if ok {
								fmt.Printf("[INFO] ✓ 成功: %s | 重试 %d 次 | 耗时: %.2fs\n",
									provider.Name, retryCount+1, duration.Seconds())
								if err := prs.blacklistService.RecordSuccess(kind, provider.Name); err != nil {
									fmt.Printf("[WARN] 清零失败计数失败: %v\n", err)
								}
								prs.setLastUsedProvider(kind, provider.Name)
								return
							}

							// 并发满载：不算尝试、不计失败，换下一个供应商
							if errors.Is(err, errProviderBusy) {
								totalAttempts--
								// 已真实失败过的供应商重试遇忙不再进等待候选：
								// 下一 pass 必然跳过它，等它只会把失败聚合错改成 503
								if pk := strconv.FormatInt(provider.ID, 10); !attemptedProviders[pk] {
									busySkipped++
									busyPending[pk] = concurrencyBusyRef{Key: pk, Limit: provider.MaxConcurrency, Gen: configGen}
								}
								fmt.Printf("[INFO] Provider %s 并发已满，跳过\n", provider.Name)
								break
							}
							// 实际尝试过：等待阶段重扫不再碰它
							attemptedProviders[strconv.FormatInt(provider.ID, 10)] = true
							delete(busyPending, strconv.FormatInt(provider.ID, 10))

							// 失败处理
							lastError = err
							lastProvider = provider.Name

							errorMsg := safeRelayError(err)
							fmt.Printf("[WARN] ✗ 失败: %s | 重试 %d/%d | 错误: %s | 耗时: %.2fs\n",
								provider.Name, retryCount+1, maxRetryPerProvider, errorMsg, duration.Seconds())

							// 客户端中断不计入失败次数，直接返回
							if errors.Is(err, errClientAbort) {
								fmt.Printf("[INFO] 客户端中断，停止重试\n")
								return
							}

							// 上游 2xx 后中途断流：响应已部分写出，不能再换供应商（会写出两段响应），
							// 但必须计入失败，否则半死的供应商永远不会被拉黑
							if errors.Is(err, errUpstreamStreamAborted) {
								trace.MarkFailed(err)
								if err := prs.blacklistService.RecordFailureWithReason(kind, provider.Name, safeRelayError(err)); err != nil {
									fmt.Printf("[ERROR] 记录失败到黑名单失败: %v\n", err)
								}
								return
							}

							// 上游判定"请求内容本身有问题"：换供应商也一样失败，
							// 不计入失败次数（否则一个坏请求能把全部供应商拉黑），直接换下一个供应商
							if errors.Is(err, errUpstreamClientError) {
								fmt.Printf("[INFO] 上游拒绝请求内容，不计供应商失败，切换到下一个\n")
								break
							}

							sawNonClientError = true

							// 记录失败次数（可能触发拉黑）
							if err := prs.blacklistService.RecordFailureWithReason(kind, provider.Name, safeRelayError(err)); err != nil {
								fmt.Printf("[ERROR] 记录失败到黑名单失败: %v\n", err)
							}
							if errors.Is(err, errUpstreamModelCapacity) {
								fmt.Printf("[INFO] Provider %s 模型容量不足，立即切换到下一个 Provider\n", provider.Name)
								break
							}

							// 检查是否刚被拉黑
							if blacklisted, _ := prs.blacklistService.IsBlacklisted(kind, provider.Name); blacklisted {
								fmt.Printf("[INFO] 🚫 Provider %s 达到失败阈值，已被拉黑，切换到下一个\n", provider.Name)
								break
							}
							if prs.isDailyCostBlocked(kind, provider) {
								fmt.Printf("[INFO] 🚫 Provider %s 已达到当日费用限额，切换到下一个\n", provider.Name)
								break
							}

							// 多地址池已在本次请求内整轮试过：不再按拉黑阈值原地重试
							// （那会放大成 阈值×地址数 次网络发送），失败已计一次，
							// 直接切下一供应商
							if errors.Is(err, errEndpointPoolExhausted) {
								fmt.Printf("[INFO] Provider %s 地址池耗尽，切换下一供应商\n", provider.Name)
								break
							}

							// 等待后重试（除非是最后一次）；等待期间客户端可能已经离开，
							// 此时继续重试只是白烧上游额度
							if retryCount < maxRetryPerProvider-1 {
								fmt.Printf("[INFO] ⏳ 等待 %d 秒后重试...\n", retryWaitSeconds)
								select {
								case <-time.After(time.Duration(retryWaitSeconds) * time.Second):
								case <-c.Request.Context().Done():
									fmt.Printf("[INFO] 等待重试期间客户端已断开，停止尝试\n")
									return
								}
							}
						}

						if c.Request.Context().Err() != nil {
							fmt.Printf("[INFO] 客户端已断开，停止尝试后续 Provider\n")
							return
						}
					}
				}

				// 一整遍下来只要还有因并发满被跳过的供应商，就进入有界等待
				if busySkipped == 0 {
					break
				}
				if busyWaitDeadline.IsZero() {
					busyWaitDeadline = time.Now().Add(prs.concurrency.waitBudget)
					if !prs.concurrency.enterWaitPhase() {
						respondAllBusy(c, kind)
						return
					}
					enteredBusyWait = true
				}
				// 唤醒以"忙候选真的有空位"为门控：本轮实际尝试供应商的正常释放
				// 也会触发全局信号，不加门控直接重扫会形成自唤醒重试风暴
				woke := false
				for {
					capSignal := prs.concurrency.releaseSignal()
					if prs.concurrency.anyCapacity(kind, busyPending) {
						woke = true
						break
					}
					if !prs.concurrency.waitForRelease(c.Request.Context(), busyWaitDeadline, capSignal) {
						break
					}
				}
				if !woke {
					respondAllBusy(c, kind)
					return
				}
				// 容量门控可能被"释放后立刻又被占走"的候选反复触发，
				// 重扫前硬校验总预算与客户端 context，防止空转越过 deadline
				if c.Request.Context().Err() != nil || time.Now().After(busyWaitDeadline) {
					respondAllBusy(c, kind)
					return
				}
				fmt.Printf("[INFO] 并发配额有释放，重扫供应商\n")
			}

			// 所有 Provider 都失败或被拉黑
			fmt.Printf("[ERROR] 💥 拉黑模式：所有 Provider 都失败或被拉黑（共尝试 %d 次）\n", totalAttempts)

			errorMsg := "未知错误"
			if lastError != nil {
				errorMsg = lastError.Error()
			}
			respondAllProvidersFailed(c, lastError, !sawNonClientError, gin.H{
				"error":         fmt.Sprintf("所有 Provider 都失败或被拉黑，最后尝试: %s - %s", lastProvider, errorMsg),
				"lastProvider":  lastProvider,
				"totalAttempts": totalAttempts,
				"mode":          "blacklist_retry",
				"hint":          "拉黑模式已开启，同 Provider 重试到拉黑再切换。如需立即降级请关闭拉黑功能",
			})
			return
		}

		// 【降级模式】：拉黑功能关闭，失败自动尝试下一个 provider
		roundRobinEnabled := prs.isRoundRobinEnabled()
		if roundRobinEnabled {
			fmt.Printf("[INFO] 🔄 降级模式 + 轮询负载均衡\n")
		} else {
			fmt.Printf("[INFO] 🔄 降级模式（顺序降级）\n")
		}

		var lastError error
		// 只要有过一次真正的供应商故障，终态就必须维持 502 让 SDK 退避重试
		sawNonClientError := false
		var lastProvider string
		var lastDuration time.Duration
		totalAttempts := 0

		busyWaitDeadline := time.Time{}
		enteredBusyWait := false
		defer func() {
			if enteredBusyWait {
				prs.concurrency.leaveWaitPhase()
			}
		}()
		busySkipped := 0
		// 已实际尝试过的供应商：等待阶段重扫不再碰它（失败已计、重试预算不重置）
		attemptedProviders := map[string]bool{}
		// 因并发满被跳过、尚未真实尝试的候选
		busyPending := map[string]concurrencyBusyRef{}
		for {
			busySkipped = 0
			// 每 pass 重建：上一轮候选可能已被拉黑或删除，残留会让容量门控恒真
			busyPending = map[string]concurrencyBusyRef{}
			for _, level := range levels {
				providersInLevel := levelGroups[level]

				// 如果启用轮询，对同 Level 的 providers 进行轮询排序
				if roundRobinEnabled {
					providersInLevel = prs.roundRobinOrder(kind, level, providersInLevel)
				}

				fmt.Printf("[INFO] === 尝试 Level %d（%d 个 provider）===\n", level, len(providersInLevel))

				for i, provider := range providersInLevel {
					if attemptedProviders[strconv.FormatInt(provider.ID, 10)] {
						continue
					}
					if blacklisted, _ := prs.blacklistService.IsBlacklisted(kind, provider.Name); blacklisted {
						continue
					}
					if prs.isDailyCostBlocked(kind, provider) {
						fmt.Printf("[INFO] ⏭️ 跳过当日额度耗尽的 Provider: %s\n", provider.Name)
						continue
					}
					totalAttempts++

					// 获取实际应该使用的模型名
					effectiveModel := provider.GetEffectiveModel(requestedModel)

					// 如果需要映射，修改请求体
					currentBodyBytes := bodyBytes
					if effectiveModel != requestedModel && requestedModel != "" {
						fmt.Printf("[INFO] Provider %s 映射模型: %s -> %s\n", provider.Name, requestedModel, effectiveModel)

						modifiedBody, err := ReplaceModelInRequestBody(bodyBytes, effectiveModel)
						if err != nil {
							trace.RecordLocalSummary(provider.Name, "model_mapping_error", "provider model mapping could not be applied")
							fmt.Printf("[ERROR] 替换模型名失败: %v\n", err)
							// 映射失败不应阻止尝试其他 provider
							continue
						}
						currentBodyBytes = modifiedBody
					}

					fmt.Printf("[INFO]   [%d/%d] Provider: %s | Model: %s\n", i+1, len(providersInLevel), provider.Name, effectiveModel)

					// 尝试发送请求
					// 获取有效的端点（用户配置优先）
					effectiveEndpoint := provider.GetEffectiveEndpoint(endpoint)
					attempt := trace.BeforeAttempt(provider.Name)
					startTime := time.Now()
					ok, err := prs.forwardRequest(c, kind, provider, effectiveEndpoint, query, clientHeaders, currentBodyBytes, isStream, effectiveModel, configGen)
					duration := time.Since(startTime)
					if err != nil {
						trace.RecordForwardError(provider.Name, err, attempt, i+1, duration)
					}

					if ok {
						fmt.Printf("[INFO]   ✓ Level %d 成功: %s | 耗时: %.2fs\n", level, provider.Name, duration.Seconds())

						// 成功：清零连续失败计数
						if err := prs.blacklistService.RecordSuccess(kind, provider.Name); err != nil {
							fmt.Printf("[WARN] 清零失败计数失败: %v\n", err)
						}

						// 记录最后使用的供应商
						prs.setLastUsedProvider(kind, provider.Name)

						return // 成功，立即返回
					}

					// 并发满载：不算尝试、不计失败，换下一个供应商
					if errors.Is(err, errProviderBusy) {
						totalAttempts--
						busySkipped++
						pk := strconv.FormatInt(provider.ID, 10)
						busyPending[pk] = concurrencyBusyRef{Key: pk, Limit: provider.MaxConcurrency, Gen: configGen}
						fmt.Printf("[INFO] Provider %s 并发已满，跳过\n", provider.Name)
						continue
					}
					// 实际尝试过：等待阶段重扫不再碰它
					attemptedProviders[strconv.FormatInt(provider.ID, 10)] = true
					delete(busyPending, strconv.FormatInt(provider.ID, 10))

					// 失败：记录错误并尝试下一个
					lastError = err
					lastProvider = provider.Name
					lastDuration = duration

					errorMsg := safeRelayError(err)
					fmt.Printf("[WARN]   ✗ Level %d 失败: %s | 错误: %s | 耗时: %.2fs\n",
						level, provider.Name, errorMsg, duration.Seconds())

					// 客户端中断不计入失败次数，且没必要再换供应商
					if errors.Is(err, errClientAbort) {
						fmt.Printf("[INFO] 客户端中断，跳过失败计数: %s\n", provider.Name)
						return
					}

					// 上游 2xx 后中途断流：响应已部分写出，不能再降级，但必须计入失败
					if errors.Is(err, errUpstreamStreamAborted) {
						trace.MarkFailed(err)
						if err := prs.blacklistService.RecordFailureWithReason(kind, provider.Name, safeRelayError(err)); err != nil {
							fmt.Printf("[ERROR] 记录失败到黑名单失败: %v\n", err)
						}
						return
					}

					// 上游判定"请求内容本身有问题"：不计入供应商失败，继续尝试下一个
					if errors.Is(err, errUpstreamClientError) {
						fmt.Printf("[INFO] 上游拒绝请求内容，不计供应商失败\n")
					} else {
						sawNonClientError = true
						if err := prs.blacklistService.RecordFailureWithReason(kind, provider.Name, safeRelayError(err)); err != nil {
							fmt.Printf("[ERROR] 记录失败到黑名单失败: %v\n", err)
						}
					}

					if c.Request.Context().Err() != nil {
						fmt.Printf("[INFO] 客户端已断开，停止尝试后续 Provider\n")
						return
					}

					// 发送切换通知：检查是否有下一个可用的 provider
					if prs.notificationService != nil {
						nextProvider := ""
						// 先查找同级别的下一个
						if i+1 < len(providersInLevel) {
							nextProvider = providersInLevel[i+1].Name
						} else {
							// 查找下一个 level 的第一个 provider
							for _, nextLevel := range levels {
								if nextLevel > level && len(levelGroups[nextLevel]) > 0 {
									nextProvider = levelGroups[nextLevel][0].Name
									break
								}
							}
						}
						if nextProvider != "" {
							prs.notificationService.NotifyProviderSwitch(SwitchNotification{
								FromProvider: provider.Name,
								ToProvider:   nextProvider,
								Reason:       errorMsg,
								Platform:     kind,
							})
						}
					}
				}

				fmt.Printf("[WARN] Level %d 的所有 %d 个 provider 均失败，尝试下一 Level\n", level, len(providersInLevel))
			}

			// 一整遍下来只要还有因并发满被跳过的供应商，就进入有界等待
			if busySkipped == 0 {
				break
			}
			if busyWaitDeadline.IsZero() {
				busyWaitDeadline = time.Now().Add(prs.concurrency.waitBudget)
				if !prs.concurrency.enterWaitPhase() {
					respondAllBusy(c, kind)
					return
				}
				enteredBusyWait = true
			}
			// 唤醒以"忙候选真的有空位"为门控：本轮实际尝试供应商的正常释放
			// 也会触发全局信号，不加门控直接重扫会形成自唤醒重试风暴
			woke := false
			for {
				capSignal := prs.concurrency.releaseSignal()
				if prs.concurrency.anyCapacity(kind, busyPending) {
					woke = true
					break
				}
				if !prs.concurrency.waitForRelease(c.Request.Context(), busyWaitDeadline, capSignal) {
					break
				}
			}
			if !woke {
				respondAllBusy(c, kind)
				return
			}
			// 容量门控可能被"释放后立刻又被占走"的候选反复触发，
			// 重扫前硬校验总预算与客户端 context，防止空转越过 deadline
			if c.Request.Context().Err() != nil || time.Now().After(busyWaitDeadline) {
				respondAllBusy(c, kind)
				return
			}
			fmt.Printf("[INFO] 并发配额有释放，重扫供应商\n")
		}

		// 所有 provider 都失败，返回 502
		errorMsg := safeRelayError(lastError)
		fmt.Printf("[ERROR] 所有 %d 个 provider 均失败，最后尝试: %s | 错误: %s\n",
			totalAttempts, lastProvider, errorMsg)

		respondAllProvidersFailed(c, lastError, !sawNonClientError, gin.H{
			"error":          fmt.Sprintf("所有 %d 个 provider 均失败，最后错误: %s", totalAttempts, errorMsg),
			"last_provider":  lastProvider,
			"last_duration":  fmt.Sprintf("%.2fs", lastDuration.Seconds()),
			"total_attempts": totalAttempts,
		})
	}
}

func (prs *ProviderRelayService) forwardRequest(
	c *gin.Context,
	kind string,
	provider Provider,
	endpoint string,
	query map[string]string,
	clientHeaders map[string]string,
	bodyBytes []byte,
	isStream bool,
	model string,
	configGen int64,
) (bool, error) {
	if err := requireCodexPlatform(kind); err != nil {
		return false, err
	}
	headers := cloneMap(clientHeaders)

	// 先清掉客户端自带的凭据与压缩协商，再注入本代理的供应商凭据
	sanitizeUpstreamHeaders(headers)

	// 请求清理（头部）：在注入供应商凭据之前执行，用户配置的黑名单删不到中继写入的认证头
	if provider.RequestSanitizeEnabled {
		headers = sanitizeHeaders(headers, provider.SanitizeConfig)
	}

	// 根据认证方式设置请求头（默认 Bearer，与 v2.2.x 保持一致）
	authType := strings.ToLower(strings.TrimSpace(provider.ConnectivityAuthType))
	switch authType {
	case "x-api-key":
		setHeaderCanonical(headers, "x-api-key", provider.APIKey)
	case "", "bearer":
		// 默认使用 Bearer token（兼容所有第三方中转）
		setHeaderCanonical(headers, "authorization", fmt.Sprintf("Bearer %s", provider.APIKey))
	default:
		// 自定义 Header 名
		headerName := strings.TrimSpace(provider.ConnectivityAuthType)
		if headerName == "" || strings.EqualFold(headerName, "custom") {
			headerName = "Authorization"
		}
		setHeaderCanonical(headers, headerName, provider.APIKey)
	}

	if getHeaderFold(headers, "accept") == "" {
		setHeaderCanonical(headers, "accept", "application/json")
	}

	// 请求清理在发送前执行，作用于实际出站 body。
	if provider.RequestSanitizeEnabled {
		if cleaned, removed := sanitizeRequestBody(bodyBytes, provider.SanitizeConfig); len(removed) > 0 {
			fmt.Printf("[Sanitize] Provider %s: 移除请求体字段 %v\n", provider.Name, removed)
			bodyBytes = cleaned
		}
	}

	// 并发配额：在本地校验/协议转换之后获取——满载时不能把本应确定
	// 返回的 400 客户端错误变成"忙"。占用覆盖地址池遍历与 SSE 转发全程
	// （本函数同步转发到流结束才返回），defer 释放即为流结束时机。
	concurrencyProviderKey := strconv.FormatInt(provider.ID, 10)
	if !prs.concurrency.TryAcquire(kind, concurrencyProviderKey, provider.MaxConcurrency, configGen) {
		return false, errProviderBusy
	}
	defer prs.concurrency.Release(kind, concurrencyProviderKey)

	requestLog := &RequestLog{
		Platform: kind,
		Provider: provider.Name,
		Model:    model,
		IsStream: isStream,
	}
	// 抓包模式（全量不脱敏）：录制终态出站 headers/body（映射/清理/认证注入均已
	// 完成，即实际进 transport 前的应用层形态），并在转发时补 URL 与响应。
	// URL/response 按"实际尝试"在地址池循环内设置，此处只做请求侧一次性采集。
	// 抓包状态一次性快照（读锁内）：开关/会话/代次分开裸读会在与关闭、清除的
	// 竞态下拼出错位组合
	if enabled, sessionID, gen := prs.captureSnapshot(); enabled {
		requestLog.captureGen = gen
		requestLog.CaptureSessionID = sessionID
		requestLog.RequestHeaders = rawRequestHeaders(headers)
		requestLog.RequestBody, requestLog.BodyTruncated, requestLog.BodyBytes = rawCaptureBody(bodyBytes)
		requestLog.respBuf = newCaptureBuffer(&prs.captureInflightBytes)
	}
	start := time.Now()
	defer func() {
		// 响应缓冲收敛进 requestLog，再归还在途抓包预算
		finalizeCaptureResponse(requestLog)
		if requestLog.respBuf != nil {
			requestLog.respBuf.release()
		}
		requestLog.DurationSec = time.Since(start).Seconds()
		// 若请求过程中发生 rename,把旧名兑换成新名再落库
		requestLog.Provider = ResolveProviderAlias(requestLog.Platform, requestLog.Provider)
		if prs.dailyLimitService != nil {
			if err := prs.dailyLimitService.SettleRequest(provider, requestLog); err != nil {
				fmt.Printf("[WARN] 更新 Provider %s 每日费用状态失败: %v\n", provider.Name, err)
			}
		}
		// 读锁覆盖"代次校验 + 提交"全程,与清除的写锁互斥,堵死校验后提交前的清除窗口
		prs.captureWriteMu.RLock()
		defer prs.captureWriteMu.RUnlock()
		prs.stripStaleCapture(requestLog)

		if err := prs.writeRequestLog(requestLog); err != nil {
			fmt.Printf("写入 request_log 失败: %v\n", err)
		}
	}()

	// ========== 地址池遍历（issue #27）==========
	// 单地址供应商：行为与旧实现完全一致（含 HTTP 层 1 次自动重试）。
	// 多地址供应商：同一请求内每个地址至多试一次，仅传输层失败/408/421/429/5xx
	// 且响应未提交时切下一地址；全部失败返回 errEndpointPoolExhausted，
	// 调用方记一次供应商失败后立即换供应商。整个池遍历共用上面这一条 requestLog。
	pool := provider.EndpointPool()
	if len(pool) == 0 {
		return false, fmt.Errorf("provider %s 没有可用的 API 地址", provider.Name)
	}
	multiAddress := len(pool) > 1
	if multiAddress {
		pool = prs.endpointCooldowns.Order(kind, provider.ID, pool)
	}

	var lastErr error
	primaryKey := normalizeURL(provider.APIURL)
	for i, addr := range pool {
		if i > 0 {
			// 上一地址的失败状态码不能残留进本次尝试的日志
			requestLog.HttpCode = 0
			// 抓包按"终态尝试"记录：切地址时丢弃上一地址的 URL/响应捕获，
			// 释放其占用的在途预算并重建缓冲，否则最终成功行会带上一地址的响应
			if requestLog.respBuf != nil {
				requestLog.RequestURL = ""
				requestLog.ResponseHeaders = ""
				requestLog.ResponseBody = ""
				requestLog.RespTruncated = false
				requestLog.RespBytes = 0
				requestLog.BudgetSkipped = false
				requestLog.respBuf.release()
				requestLog.respBuf = newCaptureBuffer(&prs.captureInflightBytes)
			}
			fmt.Printf("[INFO] Provider %s 地址兜底: 改试 %s\n", provider.Name, sanitizeLogURL(addr))
		}

		ok, err := prs.forwardToAddress(c, kind, provider, joinURL(addr, endpoint), query, headers, bodyBytes, isStream, requestLog, !multiAddress)
		if ok {
			if multiAddress {
				prs.endpointCooldowns.MarkSuccess(kind, provider.ID, addr)
				// 冷却重排后备用地址可能排在首位，不能拿下标判断主备身份
				if normalizeURL(addr) != primaryKey {
					fmt.Printf("[WARN] Provider %s 主地址失败或冷却中，备用地址 %s 接管本次请求\n", provider.Name, sanitizeLogURL(addr))
				}
			}
			return true, nil
		}

		lastErr = err
		if !multiAddress {
			return false, err
		}
		if !addressSwitchableError(err) || c.Writer.Written() {
			return false, err
		}
		prs.endpointCooldowns.MarkFailure(kind, provider.ID, addr, retryAfterOf(err))
		fmt.Printf("[WARN] Provider %s 地址 %s 失败，冷却后改试下一地址: %s\n", provider.Name, sanitizeLogURL(addr), safeRelayError(err))
	}
	return false, fmt.Errorf("%w: %v", errEndpointPoolExhausted, lastErr)
}

// forwardToAddress 向单个地址发一次请求并转发响应。
// singleAddress=true 时保留 HTTP 层 1 次自动重试（旧行为）；
// 多地址路径关闭隐藏重试，重试预算统一由地址池承担。
func (prs *ProviderRelayService) forwardToAddress(
	c *gin.Context,
	kind string,
	provider Provider,
	targetURL string,
	query map[string]string,
	headers map[string]string,
	bodyBytes []byte,
	isStream bool,
	requestLog *RequestLog,
	singleAddress bool,
) (bool, error) {
	// 绑定客户端 context：客户端取消（用户 Ctrl-C / CLI 超时断开）时立即释放上游连接，
	// 否则处理协程与上游请求会一直挂到 32 小时超时，上游还在持续产出并计费。
	// 超时与连接池由共享客户端统一提供，不再每请求新建 Transport。
	req := xrequest.New().
		SetClient(relayClientFor(provider.InsecureSkipVerify, provider.Name)).
		// SetDebug(false)：xrequest 的 debug 默认 utils.IsGoRun()，dev 下会对
		// text/json 响应预调 String() 解析并缓存，使 ToHttpResponseWriter 走
		// r.parsed 分支、绕过我们装在 RawResponse.Body 上的抓包 tee（响应体录空）。
		// 显式关掉，保证 dev 与 release 都从字节流层 tee 捕获
		SetDebug(false).
		WithContext(c.Request.Context()).
		SetHeaders(headers).
		SetQueryParams(query)
	if singleAddress {
		req = req.SetRetry(1, 500*time.Millisecond)
	}

	reqBody := bytes.NewReader(bodyBytes)
	req = req.SetBody(reqBody)

	// 抓包：记录本次实际尝试的完整 URL（含查询参数，不脱敏）
	if requestLog.respBuf != nil {
		requestLog.RequestURL = buildCaptureURL(targetURL, query)
	}

	resp, err := req.Post(targetURL)

	// 无论成功失败，先尝试记录 HttpCode 与响应头
	if resp != nil {
		requestLog.HttpCode = resp.StatusCode()
		if requestLog.respBuf != nil && resp.RawResponse != nil {
			requestLog.ResponseHeaders = rawResponseHeaders(resp.RawResponse.Header)
		}
	}

	if err != nil {
		// 客户端已断开：不是供应商故障，不计入失败次数
		if c.Request.Context().Err() != nil || errors.Is(err, context.Canceled) {
			fmt.Printf("[INFO] Provider %s 请求期间客户端已断开，不计入供应商失败\n", provider.Name)
			return false, fmt.Errorf("%w: %v", errClientAbort, err)
		}
		// 尝试从响应体提取供应商原始错误信息
		if resp != nil {
			if upstreamBody := extractUpstreamError(resp, requestLog); upstreamBody != "" {
				detail := fmt.Sprintf("upstream status %d: %s", resp.StatusCode(), upstreamBody)
				if containsModelCapacitySignal([]byte(upstreamBody)) {
					return false, newUpstreamModelCapacityError(resp, resp.StatusCode(), detail)
				}
				return false, newUpstreamStatusError(resp, resp.StatusCode(), detail)
			}
		}
		return false, err
	}

	if resp == nil {
		return false, fmt.Errorf("empty response")
	}

	status := requestLog.HttpCode

	if resp.Error() != nil {
		// 客户端已断开：不是供应商故障，不计入失败次数
		if c.Request.Context().Err() != nil {
			fmt.Printf("[INFO] Provider %s 响应期间客户端已断开，不计入供应商失败\n", provider.Name)
			return false, fmt.Errorf("%w: %v", errClientAbort, resp.Error())
		}
		// 无条件读一次错误体（同时入抓包缓冲），错误串优先用 resp.Error()，
		// 为空再回退到错误体预览
		bodyPreview := extractUpstreamError(resp, requestLog)
		errMsg := strings.TrimSpace(resp.Error().Error())
		if errMsg == "" {
			errMsg = bodyPreview
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("upstream status %d", status)
		}
		if containsModelCapacitySignal([]byte(errMsg)) || containsModelCapacitySignal([]byte(bodyPreview)) {
			return false, newUpstreamModelCapacityError(resp, status,
				fmt.Sprintf("upstream status %d: %s", status, errMsg))
		}
		if isClientSideUpstreamStatus(status) {
			return false, fmt.Errorf("%w: upstream status %d: %s", errUpstreamClientError, status, errMsg)
		}
		return false, newUpstreamStatusError(resp, status, fmt.Sprintf("upstream status %d: %s", status, errMsg))
	}

	// 状态码为 0 且无错误：当作成功处理
	if status == 0 {
		fmt.Printf("[WARN] Provider %s 返回状态码 0，但无错误，当作成功处理\n", provider.Name)
		return prs.relayResponseToClient(c, kind, provider, resp, isStream, requestLog)
	}

	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return prs.relayResponseToClient(c, kind, provider, resp, isStream, requestLog)
	}

	// 尝试从响应体提取供应商原始错误信息（同时入抓包缓冲）
	upstreamBody := extractUpstreamError(resp, requestLog)
	detail := fmt.Sprintf("upstream status %d", status)
	if upstreamBody != "" {
		detail = fmt.Sprintf("upstream status %d: %s", status, upstreamBody)
	}
	if containsModelCapacitySignal([]byte(upstreamBody)) {
		return false, newUpstreamModelCapacityError(resp, status, detail)
	}
	// 请求内容本身被拒绝：换供应商也一样，不计入供应商失败次数
	if isClientSideUpstreamStatus(status) {
		return false, fmt.Errorf("%w: %s", errUpstreamClientError, detail)
	}
	return false, newUpstreamStatusError(resp, status, detail)
}

// newUpstreamStatusError 构造带状态码的上游失败；429 时顺带解析 Retry-After
// 供地址冷却使用
func newUpstreamStatusError(resp *xrequest.Response, status int, detail string) *upstreamStatusError {
	e := &upstreamStatusError{status: status, detail: detail}
	if status == http.StatusTooManyRequests && resp != nil && resp.RawResponse != nil {
		e.retryAfter = parseRetryAfter(resp.RawResponse.Header.Get("Retry-After"), time.Now())
	}
	return e
}

func newUpstreamModelCapacityError(resp *xrequest.Response, status int, detail string) error {
	return fmt.Errorf("%w: %w", errUpstreamModelCapacity, newUpstreamStatusError(resp, status, detail))
}

// relayResponseToClient 把上游 2xx 响应转发给客户端并区分三种收尾情况：
//   - 完整转发成功；
//   - 客户端主动断开（不计供应商失败）；
//   - 上游中途断流（响应已部分写出，不能再降级，但必须计供应商失败，
//     否则半死的供应商每次都被判成功、失败计数被清零而永远不会被拉黑）。
func (prs *ProviderRelayService) relayResponseToClient(
	c *gin.Context,
	kind string,
	provider Provider,
	resp *xrequest.Response,
	isStream bool,
	requestLog *RequestLog,
) (bool, error) {
	// 抓包：在字节流层 tee 上游响应体（成功路径单协程读取，无竞态）。
	// xrequest 的逐行 hook 会剥行尾、跳空行，无法还原原始 SSE，必须在此包裹
	if requestLog.respBuf != nil && resp.RawResponse != nil && resp.RawResponse.Body != nil {
		resp.RawResponse.Body = newCaptureTeeReader(resp.RawResponse.Body, requestLog.respBuf)
	}
	responseIsSSE := resp.RawResponse != nil &&
		strings.Contains(strings.ToLower(resp.RawResponse.Header.Get("Content-Type")), "text/event-stream")
	probeWriter := newModelCapacityProbeWriter(c.Writer, responseIsSSE)
	var copyErr error
	var written int64
	written, copyErr = resp.ToHttpResponseWriter(probeWriter, RequestLogHook(c, kind, requestLog))
	if finishErr := probeWriter.Finish(); copyErr == nil {
		copyErr = finishErr
	}
	if copyErr == nil {
		if truncErr := checkNonStreamTruncated(resp, written); truncErr != nil {
			copyErr = truncErr
		}
	}

	if copyErr == nil {
		return true, nil
	}

	if c.Request.Context().Err() != nil || errors.Is(copyErr, context.Canceled) {
		fmt.Printf("[INFO] Provider %s 转发过程中客户端断开，不计入供应商失败\n", provider.Name)
		return false, fmt.Errorf("%w: %v", errClientAbort, copyErr)
	}
	if errors.Is(copyErr, errUpstreamModelCapacity) {
		capacityErr := newUpstreamModelCapacityError(resp, resp.StatusCode(), "upstream model at capacity")
		if !c.Writer.Written() {
			fmt.Printf("[WARN] Provider %s 模型容量不足，响应尚未提交，可安全降级\n", provider.Name)
			return false, capacityErr
		}
		fmt.Printf("[WARN] Provider %s 在流式响应开始后报告模型容量不足，无法拼接其它 Provider 响应\n", provider.Name)
		return false, fmt.Errorf("%w: %w", errUpstreamStreamAborted, capacityErr)
	}

	if !c.Writer.Written() {
		fmt.Printf("[WARN] Provider %s 响应读取失败且尚未写出任何内容，可降级: %s\n", provider.Name, safeTransportError(copyErr))
		return false, fmt.Errorf("upstream read failed before response started: %w", copyErr)
	}

	fmt.Printf("[WARN] Provider %s 上游中途断流（响应已部分写出，无法降级）: %s\n", provider.Name, safeTransportError(copyErr))
	return false, fmt.Errorf("%w: %v", errUpstreamStreamAborted, copyErr)
}

// RequestLogHook parses OpenAI Responses usage from streaming and non-streaming payloads.
func RequestLogHook(_ *gin.Context, kind string, usage *RequestLog) func(data []byte) (bool, []byte) {
	return func(data []byte) (bool, []byte) {
		if kind == CodexPlatform {
			parseEventPayload(strings.TrimSpace(string(data)), CodexParseTokenUsageFromResponse, usage)
		}
		return true, data
	}
}

func parseEventPayload(payload string, parser func(string, *RequestLog), usage *RequestLog) {
	hasData := false
	for _, line := range strings.Split(payload, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			hasData = true
			parser(strings.TrimSpace(strings.TrimPrefix(line, "data:")), usage)
		}
	}
	if !hasData && strings.TrimSpace(payload) != "" {
		parser(strings.TrimSpace(payload), usage)
	}
}

// CodexParseTokenUsageFromResponse parses OpenAI Responses usage snapshots.
func CodexParseTokenUsageFromResponse(data string, usage *RequestLog) {
	if usage == nil {
		return
	}
	usageResult := gjson.Get(data, "response.usage")
	if !usageResult.Exists() {
		usageResult = gjson.Get(data, "usage")
	}
	if usageResult.Exists() {
		inputTokens := int(usageResult.Get("input_tokens").Int())
		outputTokens := int(usageResult.Get("output_tokens").Int())
		cacheReadTokens := int(usageResult.Get("input_tokens_details.cached_tokens").Int())
		reasoningTokens := int(usageResult.Get("output_tokens_details.reasoning_tokens").Int())
		if cacheReadTokens > inputTokens {
			cacheReadTokens = inputTokens
		}
		if reasoningTokens > outputTokens {
			reasoningTokens = outputTokens
		}
		usage.InputTokens = inputTokens - cacheReadTokens
		usage.OutputTokens = outputTokens - reasoningTokens
		usage.CacheReadTokens = cacheReadTokens
		usage.ReasoningTokens = reasoningTokens
	}
	for _, path := range []string{"response.service_tier", "response.usage.service_tier", "service_tier", "usage.service_tier"} {
		if rawTier := gjson.Get(data, path).String(); strings.TrimSpace(rawTier) != "" {
			usage.ServiceTier = string(modelpricing.NormalizeObservedServiceTier(rawTier, warnUnknownTier))
			break
		}
	}
}

// extractUpstreamError reads and closes an upstream error response while also
// feeding the optional capture buffer.
func extractUpstreamError(resp *xrequest.Response, requestLog *RequestLog) string {
	if resp == nil {
		return ""
	}
	defer func() {
		if resp.RawResponse != nil && resp.RawResponse.Body != nil {
			_ = resp.RawResponse.Body.Close()
		}
	}()

	capturing := requestLog != nil && requestLog.respBuf != nil
	sseLimit := int64(512)
	if capturing {
		sseLimit = int64(captureFieldLimit) + 1
	}
	body := resp.String()
	if body == "" && resp.RawResponse != nil && resp.RawResponse.Body != nil {
		done := make(chan []byte, 1)
		go func() {
			raw, err := io.ReadAll(io.LimitReader(resp.RawResponse.Body, sseLimit))
			if err != nil {
				done <- nil
				return
			}
			done <- raw
		}()
		select {
		case raw := <-done:
			if raw != nil {
				body = string(raw)
			}
		case <-time.After(500 * time.Millisecond):
			_ = resp.RawResponse.Body.Close()
			if capturing {
				requestLog.respBuf.markTruncated()
			}
		}
	}
	if body == "" {
		return ""
	}
	if capturing {
		requestLog.respBuf.append([]byte(body))
	}
	if len(body) > 512 {
		return body[:512] + "..."
	}
	return body
}

func cloneHeaders(header http.Header) map[string]string {
	cloned := make(map[string]string, len(header))
	for key, values := range header {
		if len(values) > 0 {
			cloned[key] = values[len(values)-1]
		}
	}
	return cloned
}

func cloneMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func flattenQuery(values map[string][]string) map[string]string {
	query := make(map[string]string, len(values))
	for key, items := range values {
		if len(items) > 0 {
			query[key] = items[len(items)-1]
		}
	}
	return query
}

func joinURL(base string, endpoint string) string {
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(endpoint, "/")
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// ReplaceModelInRequestBody 替换请求体中的模型名
// 使用 gjson + sjson 实现高性能 JSON 操作，避免完整反序列化
func ReplaceModelInRequestBody(bodyBytes []byte, newModel string) ([]byte, error) {
	// 检查请求体中是否存在 model 字段
	result := gjson.GetBytes(bodyBytes, "model")
	if !result.Exists() {
		return bodyBytes, fmt.Errorf("请求体中未找到 model 字段")
	}

	// 使用 sjson.SetBytes 替换模型名（高性能操作）
	modified, err := sjson.SetBytes(bodyBytes, "model", newModel)
	if err != nil {
		return bodyBytes, fmt.Errorf("替换模型名失败: %w", err)
	}

	return modified, nil
}

// forwardModelsRequest 共享的 /v1/models 请求转发逻辑
// 返回 (selectedProvider, error)
func (prs *ProviderRelayService) forwardModelsRequest(
	c *gin.Context,
	kind string,
	logPrefix string,
) error {
	if err := requireCodexPlatform(kind); err != nil {
		return err
	}
	fmt.Printf("[%s] 收到 /v1/models 请求, kind=%s\n", logPrefix, kind)

	// 加载 providers
	providers, err := prs.providerService.LoadProviders(kind)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load providers"})
		return fmt.Errorf("failed to load providers: %w", err)
	}

	// 过滤可用的 providers（启用 + URL + APIKey）
	var activeProviders []Provider
	for _, provider := range providers {
		if !provider.Enabled || provider.APIURL == "" || provider.APIKey == "" {
			continue
		}

		// 黑名单检查：跳过已拉黑的 provider
		if isBlacklisted, until := prs.blacklistService.IsBlacklisted(kind, provider.Name); isBlacklisted {
			fmt.Printf("[%s] ⛔ Provider %s 已拉黑，过期时间: %v\n", logPrefix, provider.Name, until.Format("15:04:05"))
			continue
		}

		activeProviders = append(activeProviders, provider)
	}

	if len(activeProviders) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no providers available"})
		return fmt.Errorf("no providers available")
	}

	// 按 Level 分组并排序
	levelGroups := make(map[int][]Provider)
	for _, provider := range activeProviders {
		level := provider.Level
		if level <= 0 {
			level = 1
		}
		levelGroups[level] = append(levelGroups[level], provider)
	}

	levels := make([]int, 0, len(levelGroups))
	for level := range levelGroups {
		levels = append(levels, level)
	}
	sort.Ints(levels)

	// 展平候选（Level 升序、组内保持用户顺序），逐个尝试直到成功——
	// 与聊天转发一致的多 Provider 容错，避免首个供应商临时故障导致整体失败
	ordered := make([]Provider, 0, len(activeProviders))
	for _, level := range levels {
		ordered = append(ordered, levelGroups[level]...)
	}

	// 复用共享连接池；client 级 Timeout 32h 不适合模型列表这类短请求，
	// 这里用 30s 的轻量包装挂到同一个 Transport 上，按供应商选择验证策略
	modelsClient := &http.Client{Timeout: 30 * time.Second, Transport: relayHTTPClient.Transport}
	modelsClientInsecure := &http.Client{Timeout: 30 * time.Second, Transport: relayHTTPClientInsecure.Transport}
	var lastErr error
	for i := range ordered {
		selectedProvider := &ordered[i]
		client := modelsClient
		if selectedProvider.InsecureSkipVerify {
			warnInsecureProviderOnce(selectedProvider.Name)
			client = modelsClientInsecure
		}
		fmt.Printf("[%s] 使用 Provider: %s | URL: %s\n", logPrefix, selectedProvider.Name, sanitizeLogURL(selectedProvider.APIURL))

		// 地址池：与聊天转发同语义——多地址供应商按冷却排序逐地址尝试，
		// 传输失败与 408/421/429/5xx 切下一地址，凭据类 4xx 直接换供应商
		pool := selectedProvider.EndpointPool()
		multiAddress := len(pool) > 1
		if multiAddress {
			pool = prs.endpointCooldowns.Order(kind, selectedProvider.ID, pool)
		}

	addrLoop:
		for _, addr := range pool {
			// 构建目标 URL（拼接地址和 /v1/models）
			targetURL := joinURL(addr, "/v1/models")

			// 绑定客户端 context：客户端断开后不得继续遍历地址与供应商
			req, err := http.NewRequestWithContext(c.Request.Context(), "GET", targetURL, nil)
			if err != nil {
				lastErr = fmt.Errorf("failed to create request: %w", err)
				continue
			}

			// 复制客户端请求头
			for key, values := range c.Request.Header {
				for _, value := range values {
					req.Header.Add(key, value)
				}
			}
			// 清掉客户端自带凭据，避免用户本机 Key 被转发给第三方供应商
			for _, name := range clientCredentialHeaders {
				req.Header.Del(name)
			}
			for _, name := range []string{"Accept-Encoding", "Connection", "Keep-Alive", "Te", "Upgrade"} {
				req.Header.Del(name)
			}

			// 请求清理（头部）：与聊天转发同规则，在注入供应商凭据之前作用于透传的客户端头
			if selectedProvider.RequestSanitizeEnabled {
				sanitizeHTTPHeaders(req.Header, selectedProvider.SanitizeConfig)
			}

			// 根据认证方式设置请求头（默认 Bearer，与 v2.2.x 保持一致）
			authType := strings.ToLower(strings.TrimSpace(selectedProvider.ConnectivityAuthType))
			switch authType {
			case "x-api-key":
				req.Header.Set("x-api-key", selectedProvider.APIKey)
			case "", "bearer":
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", selectedProvider.APIKey))
			default:
				headerName := strings.TrimSpace(selectedProvider.ConnectivityAuthType)
				if headerName == "" || strings.EqualFold(headerName, "custom") {
					headerName = "Authorization"
				}
				req.Header.Set(headerName, selectedProvider.APIKey)
			}

			// 设置默认 Accept 头
			if req.Header.Get("Accept") == "" {
				req.Header.Set("Accept", "application/json")
			}

			resp, err := client.Do(req)
			if err != nil {
				// 客户端已断开：立即停止全部尝试，不冷却地址、不换供应商
				if c.Request.Context().Err() != nil {
					fmt.Printf("[%s] 客户端已断开，停止尝试\n", logPrefix)
					return fmt.Errorf("client aborted: %w", err)
				}
				fmt.Printf("[%s] ✗ 请求失败: %s (%s) | 错误: %s | 尝试下一个\n", logPrefix, selectedProvider.Name, sanitizeLogURL(addr), safeTransportError(err))
				lastErr = fmt.Errorf("request failed: %w", err)
				if multiAddress {
					prs.endpointCooldowns.MarkFailure(kind, selectedProvider.ID, addr, defaultEndpointCooldown)
				}
				continue // 传输层失败：可切下一地址
			}

			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				// 取消可能发生在响应头之后：错误从 body 读取冒出，
				// 同样必须即刻终止，不冷却地址、不换供应商
				if c.Request.Context().Err() != nil {
					fmt.Printf("[%s] 客户端已断开，停止尝试\n", logPrefix)
					return fmt.Errorf("client aborted: %w", readErr)
				}
				fmt.Printf("[%s] ✗ 读取响应失败: %s (%s) | 错误: %s | 尝试下一个\n", logPrefix, selectedProvider.Name, sanitizeLogURL(addr), safeTransportError(readErr))
				lastErr = fmt.Errorf("failed to read response: %w", readErr)
				if multiAddress {
					prs.endpointCooldowns.MarkFailure(kind, selectedProvider.ID, addr, defaultEndpointCooldown)
				}
				continue
			}

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				fmt.Printf("[%s] ✗ HTTP %d: %s (%s) | 尝试下一个\n", logPrefix, resp.StatusCode, selectedProvider.Name, sanitizeLogURL(addr))
				lastErr = fmt.Errorf("provider %s HTTP %d", selectedProvider.Name, resp.StatusCode)
				switchable := resp.StatusCode == http.StatusRequestTimeout ||
					resp.StatusCode == http.StatusMisdirectedRequest ||
					resp.StatusCode == http.StatusTooManyRequests ||
					resp.StatusCode >= 500
				if multiAddress && switchable {
					cooldown := defaultEndpointCooldown
					if resp.StatusCode == http.StatusTooManyRequests {
						if d := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); d > 0 {
							cooldown = d
						}
					}
					prs.endpointCooldowns.MarkFailure(kind, selectedProvider.ID, addr, cooldown)
					continue
				}
				break addrLoop // 凭据/请求类错误：换地址无意义，直接换供应商
			}

			if multiAddress {
				prs.endpointCooldowns.MarkSuccess(kind, selectedProvider.ID, addr)
			}

			// 复制响应头
			for key, values := range resp.Header {
				for _, value := range values {
					c.Header(key, value)
				}
			}

			fmt.Printf("[%s] ✓ 成功: %s (%s) | HTTP %d\n", logPrefix, selectedProvider.Name, sanitizeLogURL(addr), resp.StatusCode)
			c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
			return nil
		}
	}

	fmt.Printf("[%s] ✗ 所有 %d 个 provider 均失败 | 最后错误: %s\n", logPrefix, len(ordered), safeRelayError(lastErr))
	c.JSON(http.StatusBadGateway, gin.H{
		"error":   "all providers failed for /v1/models",
		"details": fmt.Sprintf("%v", lastErr),
	})
	return fmt.Errorf("all providers failed: %v", lastErr)
}

// modelsHandler 处理 /v1/models 请求（OpenAI-compatible API）
// 将请求转发到第一个可用的 provider 并注入 API Key
func (prs *ProviderRelayService) modelsHandler(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := requireCodexPlatform(kind); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		_ = prs.forwardModelsRequest(c, kind, "Models")
	}
}

// ========== 请求清理（Request Sanitizer，黑名单模式，按供应商开启） ==========

// 内置默认黑名单：供应商对应维度未配置（nil）时使用；
// 显式配置为空数组表示该维度什么都不删。
var (
	defaultBlockedBodyFields = []string{"prompt_caching"}
	defaultBlockedHeaders    []string
)

// resolveBlocklist 把自定义列表（nil 时退回默认列表）转成查找集合。
// fold 为 true 时按小写归一（用于大小写不敏感的请求头名）。
func resolveBlocklist(custom, def []string, fold bool) map[string]bool {
	src := custom
	if src == nil {
		src = def
	}
	m := make(map[string]bool, len(src))
	for _, v := range src {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if fold {
			v = strings.ToLower(v)
		}
		m[v] = true
	}
	return m
}

// cfg 三个访问器把指针三态展开成切片语义：
// nil 指针 → 返回 nil（调用方回退内置默认）；指向空数组 → 返回非 nil 空切片（什么都不删）。
func cfgBodyFields(cfg *SanitizeConfig) []string {
	if cfg == nil || cfg.BlockedBodyFields == nil {
		return nil
	}
	return derefList(cfg.BlockedBodyFields)
}

func cfgHeaders(cfg *SanitizeConfig) []string {
	if cfg == nil || cfg.BlockedHeaders == nil {
		return nil
	}
	return derefList(cfg.BlockedHeaders)
}

// derefList 解引用并保证返回非 nil 切片，避免"指向 nil 切片的指针"退化回默认列表。
func derefList(p *[]string) []string {
	if *p == nil {
		return []string{}
	}
	return *p
}

// sanitizeRequestBody 移除请求体顶层黑名单字段，返回清理后的 body 与被移除的键。
// 单趟重建：一次解析、一次序列化；顶层键序可能变化，JSON 语义不受影响。
// 顶层存在重复键的畸形 body 原样放行——map 解析会静默吞并重复键，
// 宁可不清理也不能改写非目标数据。
func sanitizeRequestBody(bodyBytes []byte, cfg *SanitizeConfig) ([]byte, []string) {
	blocked := resolveBlocklist(cfgBodyFields(cfg), defaultBlockedBodyFields, false)
	if len(blocked) == 0 {
		return bodyBytes, nil
	}

	root := gjson.ParseBytes(bodyBytes)
	if !root.IsObject() {
		return bodyBytes, nil
	}
	// 快速路径：统计顶层键出现次数，没有命中黑名单就不动 body
	hasBlocked := false
	keyCount := 0
	root.ForEach(func(key, _ gjson.Result) bool {
		keyCount++
		if blocked[key.String()] {
			hasBlocked = true
		}
		return true
	})
	if !hasBlocked {
		return bodyBytes, nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &fields); err != nil {
		return bodyBytes, nil
	}
	if len(fields) != keyCount {
		fmt.Printf("[Sanitize] 请求体顶层存在重复键，跳过清理以免改写非目标数据\n")
		return bodyBytes, nil
	}

	var removed []string
	for k := range fields {
		if blocked[k] {
			removed = append(removed, k)
			delete(fields, k)
		}
	}
	cleaned, err := json.Marshal(fields)
	if err != nil {
		return bodyBytes, nil
	}
	sort.Strings(removed)
	return cleaned, removed
}

// sanitizeHeaders 移除黑名单请求头。
// 必须在注入供应商凭据之前调用，用户配置的黑名单才碰不到中继写入的认证头。
func sanitizeHeaders(headers map[string]string, cfg *SanitizeConfig) map[string]string {
	blockedHeader := resolveBlocklist(cfgHeaders(cfg), defaultBlockedHeaders, true)
	cleaned := make(map[string]string, len(headers))
	for k, v := range headers {
		lower := strings.ToLower(k)
		if blockedHeader[lower] {
			continue
		}
		cleaned[k] = v
	}
	return cleaned
}

// sanitizeHTTPHeaders 是 sanitizeHeaders 的 http.Header 版本，供 models 转发路径使用。
func sanitizeHTTPHeaders(h http.Header, cfg *SanitizeConfig) {
	blockedHeader := resolveBlocklist(cfgHeaders(cfg), defaultBlockedHeaders, true)
	for _, k := range headerKeys(h) {
		lower := strings.ToLower(k)
		if blockedHeader[lower] {
			h.Del(k)
			continue
		}
	}
}

// headerKeys 先收集键再遍历，避免边遍历边删除。
func headerKeys(h http.Header) []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	return keys
}
