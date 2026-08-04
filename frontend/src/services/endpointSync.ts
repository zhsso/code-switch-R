/**
 * 端点同步服务
 * 从 Codex 平台获取供应商 API 端点
 * @author sm
 */

import { LoadProviders } from './providers'

/**
 * 同步的端点数据结构
 */
export interface SyncedEndpoint {
  url: string              // 标准化的基础 URL
  source: 'codex'          // 来源平台
  providerName: string     // 供应商名称
}

/**
 * 提取 API URL 的基础地址（去除路径部分）
 * 例如: https://api.openai.com/v1/chat/completions -> https://api.openai.com
 * @author sm
 */
function extractBaseUrl(apiUrl: string): string {
  if (!apiUrl || !apiUrl.trim()) {
    return ''
  }

  try {
    const url = new URL(apiUrl)
    return `${url.protocol}//${url.host}`
  } catch {
    const trimmed = apiUrl.trim()
    const versionIndex = trimmed.indexOf('/v1')
    if (versionIndex > 0) {
      return trimmed.substring(0, versionIndex)
    }
    console.warn('无效的 API URL:', apiUrl)
    return ''
  }
}

/**
 * 从所有供应商服务获取端点列表
 * @author sm
 */
export async function fetchAllProviderEndpoints(): Promise<SyncedEndpoint[]> {
  const endpoints: SyncedEndpoint[] = []

  try {
    const codexProviders = await LoadProviders('codex')
    if (Array.isArray(codexProviders)) {
      codexProviders.forEach((p: any) => {
        if (p.apiUrl && p.apiUrl.trim()) {
          const baseUrl = extractBaseUrl(p.apiUrl)
          if (baseUrl) {
            endpoints.push({
              url: baseUrl,
              source: 'codex',
              providerName: p.name || 'Codex Provider'
            })
          }
        }
      })
    }
  } catch (error) {
    console.error('获取 Codex 供应商失败:', error)
    throw new Error('供应商端点同步失败')
  }

  const uniqueEndpoints = new Map<string, SyncedEndpoint>()
  endpoints.forEach(ep => {
    if (!uniqueEndpoints.has(ep.url)) {
      uniqueEndpoints.set(ep.url, ep)
    }
  })

  return Array.from(uniqueEndpoints.values())
}
