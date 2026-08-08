import { Call } from '../runtime'

export type DailyCostLimitStatus = {
  providerId: string
  providerName: string
  enabled: boolean
  timezone: string
  day: string
  limitMicros: number
  systemCostMicros: number
  manualAdjustmentMicros: number
  usedMicros: number
  usagePercent: number
  autoBlocked: boolean
  manualBlocked: boolean
  blocked: boolean
  blockReason: '' | 'quota' | 'manual' | 'quota_and_manual' | string
}

const SERVICE = 'codeswitch/services.DailyCostLimitService'

export const fetchDailyCostLimitStatuses = async (platform = 'codex') =>
  (await Call.ByName<DailyCostLimitStatus[]>(`${SERVICE}.GetStatuses`, platform)) ?? []

export const setDailyActualUsage = (platform: string, providerId: string, actualMicros: number) =>
  Call.ByName(`${SERVICE}.SetActualUsage`, platform, providerId, actualMicros)

export const manuallyBlockDaily = (platform: string, providerId: string) =>
  Call.ByName(`${SERVICE}.ManualBlock`, platform, providerId)

export const temporarilyUnblockDaily = (platform: string, providerId: string) =>
  Call.ByName(`${SERVICE}.TemporaryUnblock`, platform, providerId)
