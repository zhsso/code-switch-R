import { Call } from '../runtime'

export type EndpointLatency = {
  url: string
  latency: number | null
  status?: number
  error?: string
}

export const TestEndpoints = (urls: string[], timeoutSecs?: number): Promise<EndpointLatency[]> =>
  Call.ByName('codeswitch/services.SpeedTestService.TestEndpoints', urls, timeoutSecs ?? null)
