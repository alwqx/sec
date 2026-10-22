import type { BasicSecurity, SecurityQuote } from './types'

async function request<T>(path: string, signal?: AbortSignal): Promise<T> {
  const resp = await fetch(path, { signal })
  if (!resp.ok) {
    const body = await resp.json().catch(() => null)
    throw new Error(body?.error ?? `请求失败 (${resp.status})`)
  }
  return resp.json() as Promise<T>
}

export async function search(key: string): Promise<BasicSecurity[]> {
  const data = await request<{ results: BasicSecurity[] }>(
    `/api/v1/search?key=${encodeURIComponent(key)}`,
  )
  return data.results
}

export async function quote(code: string, signal?: AbortSignal): Promise<SecurityQuote> {
  return request<SecurityQuote>(`/api/v1/quote?code=${encodeURIComponent(code)}`, signal)
}
