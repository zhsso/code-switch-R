import { Call } from '../runtime'

// 黑名单状态接口
export interface BlacklistStatus {
  platform: string
  providerName: string
  failureCount: number
  blacklistedAt?: string  // ISO 时间字符串
  blacklistedUntil?: string  // ISO 时间字符串
  lastFailureAt?: string  // ISO 时间字符串
  isBlacklisted: boolean
  remainingSeconds: number  // 剩余拉黑时间（秒）

  // v0.4.0 新增：等级拉黑相关字段
  blacklistLevel: number          // 当前黑名单等级 (0-5)
  lastRecoveredAt?: string        // 最后恢复时间（ISO 时间字符串）
  forgivenessRemaining: number    // 距离宽恕还剩多少秒（3小时倒计时）
  lastFailureReason?: string      // 最近一次失败摘要
}

// 黑名单配置接口
export interface BlacklistSettings {
  failureThreshold: number  // 失败次数阈值
  durationMinutes: number   // 拉黑时长（分钟）
}

const BLACKLIST_SERVICE = 'codeswitch/services.BlacklistService'
const SETTINGS_SERVICE = 'codeswitch/services.SettingsService'

/**
 * 获取 Codex 黑名单状态列表
 */
export const getBlacklistStatus = async (): Promise<BlacklistStatus[]> => {
  return Call.ByName(`${BLACKLIST_SERVICE}.GetBlacklistStatus`, 'codex')
}

/**
 * 手动解除拉黑
 * @param providerName provider 名称
 * @param providerName provider 名称
 */
export const manualUnblock = async (providerName: string): Promise<void> => {
  return Call.ByName(`${BLACKLIST_SERVICE}.ManualUnblock`, 'codex', providerName)
}

/**
 * 获取黑名单配置
 */
export const getBlacklistSettings = async (): Promise<BlacklistSettings> => {
  return Call.ByName(`${SETTINGS_SERVICE}.GetBlacklistSettingsStruct`)
}

/**
 * 更新黑名单配置
 * @param threshold 失败次数阈值（1-10）
 * @param duration 固定拉黑时长（1-10080 分钟）
 */
export const updateBlacklistSettings = async (threshold: number, duration: number): Promise<void> => {
  return Call.ByName(`${SETTINGS_SERVICE}.UpdateBlacklistSettings`, threshold, duration)
}
