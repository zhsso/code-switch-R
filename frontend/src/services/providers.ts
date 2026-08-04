import { Call } from '../runtime'

const SERVICE = 'codeswitch/services.ProviderService'

export type ProviderSnapshot<T = any> = {
  providers: T[]
  generation: number
}

export const LoadProviders = async <T = any>(kind: string): Promise<ProviderSnapshot<T>> => {
  return (await Call.ByName<ProviderSnapshot<T>>(`${SERVICE}.LoadProviders`, kind)) ?? {
    providers: [],
    generation: 0,
  }
}

export const SaveProviders = async (kind: string, generation: number, providers: unknown[]): Promise<number> => {
  return Call.ByName<number>(`${SERVICE}.SaveProviders`, kind, generation, providers)
}

export const RevealProviderAPIKey = async (kind: string, id: number): Promise<string> => {
  return Call.ByName<string>(`${SERVICE}.RevealProviderAPIKey`, kind, id)
}

export const DuplicateProvider = async <T = any>(kind: string, sourceID: number): Promise<T> => {
  return Call.ByName<T>(`${SERVICE}.DuplicateProvider`, kind, sourceID)
}

export const RenameProvider = async (kind: string, id: number, name: string): Promise<number> => {
  return Call.ByName<number>(`${SERVICE}.RenameProvider`, kind, id, name)
}
