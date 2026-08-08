export type AutomationCard = {
  id: number
  name: string
  apiUrl: string
  apiKey: string
  apiKeyConfigured?: boolean
  apiKeyChanged?: boolean
  officialSite: string
  icon: string
  tint: string
  accent: string
  enabled: boolean
  // 模型映射：external model -> internal model
  modelMapping?: Record<string, string>
  // API 端点路径（可选）：覆盖平台默认端点
  apiEndpoint?: string
  // 备用 API 地址（最多 4 个）：主地址失败时同一请求内按序兜底
  fallbackApiUrls?: string[]
  // 费用统计倍率（缺失时按 1）
  costMultiplier?: number
  // 每日费用限额（微美元，1 USD = 1,000,000）
  dailyCostLimitEnabled?: boolean
  dailyCostLimitMicros?: number
  // === 可用性监控配置（新） ===
  // 可用性监控开关：是否启用后台健康检查
  availabilityMonitorEnabled?: boolean
  // 连通性自动拉黑：检测失败时是否自动拉黑该供应商
  connectivityAutoBlacklist?: boolean
  // 可用时自动解禁：普通黑名单内连续检测成功后提前恢复
  availabilityAutoUnblock?: boolean
  // 可用性高级配置：测试模型、端点、超时和后台检测间隔
  availabilityConfig?: {
    testModel?: string            // 测试用模型
    testEndpoint?: string         // 测试端点路径
    timeout?: number              // 超时时间（毫秒）
    pollIntervalSeconds?: number  // 后台检测间隔（秒）
  }

  // 跳过上游 TLS 证书验证（仅该供应商，自签名/企业代理场景；存在中间人风险）
  insecureSkipVerify?: boolean

  // 请求清理：启用后转发前移除非标准字段和请求头（黑名单模式）
  requestSanitizeEnabled?: boolean
  sanitizeConfig?: {
    blockedBodyFields?: string[]
    blockedHeaders?: string[]
  }

  // === 旧连通性字段（已废弃，仅用于兼容旧数据） ===
  /** @deprecated 已迁移到 availabilityMonitorEnabled */
  connectivityCheck?: boolean
  /** @deprecated 已迁移到 availabilityConfig.testModel */
  connectivityTestModel?: string
  /** @deprecated 已迁移到 availabilityConfig.testEndpoint */
  connectivityTestEndpoint?: string
  /** @deprecated 已迁移到可用性配置中的认证方式 */
  connectivityAuthType?: string
}

export const automationCardGroups: Record<'codex', AutomationCard[]> = {
  codex: [
    {
      id: 201,
      name: 'AICoding.sh',
      apiUrl: 'https://api.aicoding.sh',
      apiKey: '',
      officialSite: 'https://www.aicoding.sh',
      icon: 'aicoding',
      tint: 'rgba(236, 72, 153, 0.16)',
      accent: '#ec4899',
      enabled: false,
    },
  ],
}

export function createAutomationCards(data: AutomationCard[] = []): AutomationCard[] {
  return data.map((item) => ({
    ...item,
    officialSite: item.officialSite ?? '',
    costMultiplier: item.costMultiplier && item.costMultiplier > 0 ? item.costMultiplier : 1,
  }))
}
