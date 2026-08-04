type RPCEnvelope<T> = {
  result?: T
  error?: string
}

export const Call = {
  async ByName<T = any>(method: string, ...args: unknown[]): Promise<T> {
    const response = await fetch('/api/rpc', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ method, args }),
    })

    let envelope: RPCEnvelope<T>
    try {
      envelope = await response.json() as RPCEnvelope<T>
    } catch {
      throw new Error(`RPC ${method} returned HTTP ${response.status}`)
    }
    if (!response.ok || envelope.error) {
      throw new Error(envelope.error || `RPC ${method} failed with HTTP ${response.status}`)
    }
    return envelope.result as T
  },
}

export type ServerEvent<T = unknown> = {
  name: string
  data: T
}

type EventCallback = (event: ServerEvent<any>) => void

let eventSource: EventSource | null = null
let listenerCount = 0

function source(): EventSource {
  if (!eventSource) {
    eventSource = new EventSource('/api/events')
  }
  return eventSource
}

export const Events = {
  On(name: string, callback: EventCallback): () => void {
    const stream = source()
    const listener: EventListener = (raw) => {
      const message = raw as MessageEvent<string>
      let data: unknown = null
      try {
        data = JSON.parse(message.data)
      } catch {
        data = message.data
      }
      callback({ name, data })
    }
    stream.addEventListener(name, listener)
    listenerCount += 1

    let active = true
    return () => {
      if (!active) return
      active = false
      stream.removeEventListener(name, listener)
      listenerCount -= 1
      if (listenerCount === 0 && eventSource) {
        eventSource.close()
        eventSource = null
      }
    }
  },
}
