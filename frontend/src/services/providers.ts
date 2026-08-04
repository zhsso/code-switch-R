import { Call } from '../runtime'

const SERVICE = 'codeswitch/services.ProviderService'

export const LoadProviders = async <T = any>(kind: string): Promise<T[]> => {
  return (await Call.ByName<T[]>(`${SERVICE}.LoadProviders`, kind)) ?? []
}

export const SaveProviders = async (kind: string, providers: unknown[]): Promise<void> => {
  await Call.ByName(`${SERVICE}.SaveProviders`, kind, providers)
}

export const RevealProviderAPIKey = async (kind: string, id: number): Promise<string> => {
  return Call.ByName<string>(`${SERVICE}.RevealProviderAPIKey`, kind, id)
}

export const DuplicateProvider = async <T = any>(kind: string, sourceID: number): Promise<T> => {
  return Call.ByName<T>(`${SERVICE}.DuplicateProvider`, kind, sourceID)
}

export const RenameProvider = async (kind: string, id: number, name: string): Promise<void> => {
  await Call.ByName(`${SERVICE}.RenameProvider`, kind, id, name)
}
