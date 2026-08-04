import { Call } from '../runtime'

export type ModelSyncStatus = {
  autoSyncEnabled: boolean
  running: boolean
  generation: number
  lastSuccess: string
  providers: Array<{
    provider: string
    source: string
    modelCount: number
    lastSuccess?: string
    lastError?: string
  }>
  pricing: {
    totalModels?: number
    TotalModels?: number
  }
  defaultModels: {
    codexDefault: string
  }
}

const SERVICE = 'codeswitch/services.ModelSyncService'

export const GetSyncStatus = (): Promise<ModelSyncStatus> =>
  Call.ByName(`${SERVICE}.GetSyncStatus`)

export const SyncNow = (): Promise<ModelSyncStatus> =>
  Call.ByName(`${SERVICE}.SyncNow`)

export const RestoreBuiltinPricing = (): Promise<ModelSyncStatus> =>
  Call.ByName(`${SERVICE}.RestoreBuiltinPricing`)
