import type { BasicSecurity, SecurityQuote, Company, Dividend, WorkspaceState, PageResult, IPOListing, Announcement, FinancialReport, MetalData, HistoryQuote, BondData, CompanyMetrics, SavedDownload, AnnualReportsPage, ProspectusPage } from './types'

async function request<T>(path: string, signal?: AbortSignal, options?: RequestInit): Promise<T> {
  const resp = await fetch(path, { ...options, signal })
  if (!resp.ok) {
    const body = await resp.json().catch(() => null)
    throw new Error(body?.error ?? `请求失败 (${resp.status})`)
  }
  return resp.json() as Promise<T>
}

export async function search(key: string, signal?: AbortSignal): Promise<BasicSecurity[]> {
  const data = await request<{ results: BasicSecurity[] }>(
    `/api/v1/search?key=${encodeURIComponent(key)}`, signal,
  )
  return data.results
}

export async function quote(code: string, signal?: AbortSignal): Promise<SecurityQuote> {
  return request<SecurityQuote>(`/api/v1/quote?code=${encodeURIComponent(code)}`, signal)
}

export function company(code: string, signal?: AbortSignal): Promise<Company> {
  return request(`/api/v1/company?code=${encodeURIComponent(code)}`, signal)
}

export async function dividends(code: string, signal?: AbortSignal): Promise<Dividend[]> {
  const data = await request<{ results: Dividend[] }>(
    `/api/v1/dividends?code=${encodeURIComponent(code)}`, signal,
  )
  return data.results
}

export function loadWorkspace(signal?: AbortSignal): Promise<WorkspaceState> {
  return request('/api/v1/workspace', signal)
}

export function saveWorkspace(state: WorkspaceState): Promise<WorkspaceState> {
  return request('/api/v1/workspace', undefined, {
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(state),
  })
}

export function ipo(page: number, signal?: AbortSignal): Promise<PageResult<IPOListing>> {
  return request(`/api/v1/ipo?page=${page}`, signal)
}

export function announcements(page: number, code: string, key: string, type: string, signal?: AbortSignal): Promise<PageResult<Announcement>> {
  const params = new URLSearchParams({ page: String(page), key, type })
  if (code) params.set('code', code)
  return request(`/api/v1/announcements?${params}`, signal)
}

export function financials(code: string, report: string, period: string, signal?: AbortSignal): Promise<FinancialReport> {
  return request(`/api/v1/financials?${new URLSearchParams({ code, report, period })}`, signal)
}

export async function history(code: string, filters: { begin: string; end: string; fq: string }, signal?: AbortSignal): Promise<HistoryQuote[]> {
  const data = await request<{ results: HistoryQuote[] }>(`/api/v1/history?${new URLSearchParams({ code, ...filters })}`, signal)
  return data.results
}

export function metal(begin: string, end: string, signal?: AbortSignal): Promise<MetalData> {
  return request(`/api/v1/metal?${new URLSearchParams({ begin, end })}`, signal)
}

export function bond(begin: string, end: string, signal?: AbortSignal): Promise<BondData> {
  return request(`/api/v1/bond?${new URLSearchParams({ begin, end })}`, signal)
}

export function companyMetrics(code: string, signal?: AbortSignal): Promise<CompanyMetrics> {
  return request(`/api/v1/metrics?${new URLSearchParams({ code })}`, signal)
}

export function exportFinancials(code: string, report: string, period: string, format: 'csv' | 'json', signal?: AbortSignal): Promise<SavedDownload> {
  return request('/api/v1/financials/export', signal, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code, report, period, format }),
  })
}

export function annualReports(code: string, startYear: number, endYear: number, page: number, signal?: AbortSignal): Promise<AnnualReportsPage> {
  return request(`/api/v1/annual-reports?${new URLSearchParams({ code, start_year: String(startYear), end_year: String(endYear), page: String(page) })}`, signal)
}

export function downloadAnnualReport(code: string, year: number, url: string, signal?: AbortSignal): Promise<SavedDownload> {
  return request('/api/v1/annual-reports/download', signal, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code, year: String(year), url }),
  })
}

export function prospectuses(code: string, begin: string, end: string, page: number, signal?: AbortSignal): Promise<ProspectusPage> {
  return request(`/api/v1/ipo/prospectuses?${new URLSearchParams({ code, begin, end, page: String(page) })}`, signal)
}

export function downloadProspectus(code: string, url: string, signal?: AbortSignal): Promise<SavedDownload> {
  return request('/api/v1/ipo/prospectuses/download', signal, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code, url }),
  })
}
