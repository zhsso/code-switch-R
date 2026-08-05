import { Call } from '../runtime'

export type AppSettings = {
  show_heatmap: boolean
  show_home_title: boolean
  auto_sync_models: boolean
  auto_connectivity_test: boolean
  enable_switch_notify: boolean
  enable_round_robin: boolean
  history_retention_days: number
  timezone: string
}

const DEFAULT_SETTINGS: AppSettings = {
  show_heatmap: true,
  show_home_title: true,
  auto_sync_models: true,
  auto_connectivity_test: true,
  enable_switch_notify: true,
  enable_round_robin: false,
  history_retention_days: 30,
  timezone: 'Asia/Shanghai',
}

export const fetchAppSettings = async (): Promise<AppSettings> => {
  const data = await Call.ByName('codeswitch/services.AppSettingsService.GetAppSettings')
  return { ...DEFAULT_SETTINGS, ...(data ?? {}) }
}

export const saveAppSettings = async (settings: AppSettings): Promise<AppSettings> => {
  return Call.ByName('codeswitch/services.AppSettingsService.SaveAppSettings', settings)
}
