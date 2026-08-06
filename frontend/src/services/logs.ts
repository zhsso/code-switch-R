import { Call } from '../runtime'

export type LogPlatform = 'codex'

export type RequestEventType =
  | 'incident'
  | 'all'
  | 'request_error'
  | 'provider_switch'
  | 'request_completed'

export type RequestLog = {
  id: number
  platform: LogPlatform
  model: string
  provider: string
  http_code: number
  input_tokens: number
  output_tokens: number
  cache_create_tokens: number
  cache_read_tokens: number
  reasoning_tokens: number
  is_stream?: boolean | number
  duration_sec?: number
  created_at: string
  total_cost?: number
  input_cost?: number
  output_cost?: number
  cache_create_cost?: number
  cache_read_cost?: number
  has_pricing?: boolean
  has_capture?: boolean
}

// 抓包详情：仅当抓包模式开启时该行才有内容，按需单独拉取
export type RequestLogDetail = {
  id: number
  platform: string
  provider: string
  model: string
  created_at: string
  request_url: string
  request_headers: string
  request_body: string
  request_body_preview: boolean
  body_truncated: boolean
  body_bytes: number
  response_headers: string
  response_body: string
  response_body_preview: boolean
  response_truncated: boolean
  response_bytes: number
  budget_skipped: boolean
}

export const fetchRequestLogDetail = async (id: number): Promise<RequestLogDetail> => {
  return Call.ByName('codeswitch/services.LogService.GetRequestLogDetail', id)
}

type RequestLogQuery = {
  platform?: LogPlatform
  provider?: string
  limit?: number
}

export const fetchRequestLogs = async (query: RequestLogQuery = {}): Promise<RequestLog[]> => {
  const platform = query.platform ?? 'codex'
  const provider = query.provider ?? ''
  const limit = query.limit ?? 100
  return Call.ByName('codeswitch/services.LogService.ListRequestLogs', platform, provider, limit)
}

export const fetchLogProviders = async (platform: LogPlatform = 'codex'): Promise<string[]> => {
  return Call.ByName('codeswitch/services.LogService.ListProviders', platform)
}

export type RequestEvent = {
  id: number
  request_id: string
  platform: LogPlatform
  model: string
  event_type: 'request_error' | 'provider_switch' | 'request_completed' | string
  provider: string
  from_provider: string
  to_provider: string
  attempt: number
  retry: number
  http_code: number
  error_type: string
  error_code: string
  message: string
  duration_sec: number
  outcome: string
  created_at: string
}

export type RequestEventQuery = {
  platform?: LogPlatform
  eventType?: RequestEventType
  provider?: string
  requestId?: string
  days?: number
  limit?: number
  offset?: number
}

export const fetchRequestEvents = async (query: RequestEventQuery = {}): Promise<RequestEvent[]> => {
  const platform = query.platform ?? 'codex'
  const eventType = query.eventType ?? 'incident'
  const provider = query.provider ?? ''
  const requestId = query.requestId ?? ''
  const days = query.days ?? 30
  const limit = query.limit ?? 50
  const offset = query.offset ?? 0
  return Call.ByName(
    'codeswitch/services.LogService.ListRequestEvents',
    platform,
    eventType,
    provider,
    requestId,
    days,
    limit,
    offset,
  )
}

export type LogStatsSeries = {
  day: string
  total_requests: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cache_create_tokens: number
  cache_read_tokens: number
  total_cost: number
}

export type LogStats = {
  total_requests: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cache_create_tokens: number
  cache_read_tokens: number
  cost_total: number
  cost_input: number
  cost_output: number
  cost_cache_create: number
  cost_cache_read: number
  series: LogStatsSeries[]
}

export const fetchLogStats = async (platform: LogPlatform = 'codex'): Promise<LogStats> => {
  return Call.ByName('codeswitch/services.LogService.StatsSince', platform)
}

export const fetchCostSince = async (start: string, platform: LogPlatform = 'codex'): Promise<number> => {
  return Call.ByName('codeswitch/services.LogService.CostSince', start, platform)
}

export type ProviderDailyStat = {
  provider: string
  total_requests: number
  successful_requests: number
  failed_requests: number
  success_rate: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cache_create_tokens: number
  cache_read_tokens: number
  cost_total: number
}

export const fetchProviderDailyStats = async (
  platform: LogPlatform = 'codex',
): Promise<ProviderDailyStat[]> => {
  return Call.ByName('codeswitch/services.LogService.ProviderDailyStats', platform)
}

export type HeatmapStat = {
  day: string
  total_requests: number
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  total_cost: number
}

export const fetchHeatmapStats = async (days: number): Promise<HeatmapStat[]> => {
  const range = Number.isFinite(days) && days > 0 ? Math.floor(days) : 30
  return Call.ByName('codeswitch/services.LogService.HeatmapStats', range)
}
