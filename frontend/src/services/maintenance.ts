import { Call } from '../runtime'

export type HistoryCleanupResult = {
  retention_days: number
  request_logs: number
  health_checks: number
}

export const cleanupConfiguredHistory = (): Promise<HistoryCleanupResult> =>
  Call.ByName('codeswitch/services.MaintenanceService.CleanupConfiguredHistory')
