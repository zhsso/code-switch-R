import type { ProviderCompatibilityMode } from './cards'

type ProviderPreset = {
  name: string
  apiUrl: string
  officialSite: string
  icon: string
  level: number
  enabled: boolean
  supportedModels: Record<string, boolean>
  modelMapping: Record<string, string>
  apiEndpoint: string
  compatibilityMode: ProviderCompatibilityMode
  availabilityConfig: {
    testModel: string
    testEndpoint: string
    timeout: number
  }
}

export const createDeepSeekV4FlashPreset = (): ProviderPreset => ({
  name: 'DeepSeek V4 Flash',
  apiUrl: 'https://api.deepseek.com',
  officialSite: 'https://platform.deepseek.com',
  icon: 'deepseek',
  level: 1,
  enabled: true,
  supportedModels: { 'deepseek-v4-flash': true },
  modelMapping: { 'gpt-5.6-sol': 'deepseek-v4-flash' },
  apiEndpoint: '/responses',
  compatibilityMode: 'deepseek-codex',
  availabilityConfig: {
    testModel: 'deepseek-v4-flash',
    testEndpoint: '/responses',
    timeout: 15000,
  },
})
