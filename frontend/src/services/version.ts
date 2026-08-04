export const fetchCurrentVersion = async (): Promise<string> => {
  const response = await fetch('/api/info')
  if (!response.ok) throw new Error(`HTTP ${response.status}`)
  const info = await response.json() as { version?: string }
  return info.version ?? ''
}
