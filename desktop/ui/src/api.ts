import type { BasicSecurity, SecurityQuote, Company, Dividend, WorkspaceState, PageResult, IPOListing, Announcement, FinancialReport, MetalData, SECFilings, USCompanyData, HKCompanyData, FundOverview, USFinancialsData, CommodityQuote, CommodityHistory, HistoryQuote, BondData, CompanyMetrics, SavedDownload, AnnualReportsPage, ProspectusPage , MacroCatalog, MacroDataset , FxDataset, FxSpread , HistoryOverlay , StrategyResult, ValuationResult, FinancialCharts, USDividends } from './types'

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
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ settings: state.settings }),
  })
}

export function ipo(page: number, since: string, until: string, signal?: AbortSignal): Promise<PageResult<IPOListing>> {
  return request(`/api/v1/ipo?${new URLSearchParams({ page: String(page), since, until })}`, signal)
}

export function announcements(page: number, code: string, key: string, type: string, signal?: AbortSignal): Promise<PageResult<Announcement>> {
  const params = new URLSearchParams({ page: String(page), key, type })
  if (code) params.set('code', code)
  return request(`/api/v1/announcements?${params}`, signal)
}

export function financialCharts(code: string, freq: 'annual' | 'quarter', signal?: AbortSignal): Promise<FinancialCharts> {
  return request(`/api/v1/financials/charts?${new URLSearchParams({ code, freq })}`, signal)
}

export function usFinancialCharts(code: string, freq: 'annual' | 'quarter', signal?: AbortSignal): Promise<FinancialCharts> {
  return request(`/api/v1/us/financials/charts?${new URLSearchParams({ code, freq })}`, signal)
}

export function financials(code: string, report: string, period: string, signal?: AbortSignal): Promise<FinancialReport> {
  return request(`/api/v1/financials?${new URLSearchParams({ code, report, period })}`, signal)
}

export function history(code: string, filters: { begin: string; end: string; fq: string; freq?: string; ma?: string; boll?: string }, signal?: AbortSignal): Promise<{ results: HistoryQuote[]; overlays?: HistoryOverlay[]; source_note?: string }> {
  return request(`/api/v1/history?${new URLSearchParams({ code, ...filters })}`, signal)
}

export function quotes(codes: string[], signal?: AbortSignal): Promise<{ results: SecurityQuote[]; missing: string[] }> {
  return request(`/api/v1/quotes?${new URLSearchParams({ codes: codes.join(',') })}`, signal)
}

// 后端缓存上海黄金交易所的数据 10 分钟；refresh 为 true 时重新请求。
export function metal(inst: string, begin: string, end: string, refresh: boolean, signal?: AbortSignal): Promise<MetalData> {
  return request(`/api/v1/metal?${new URLSearchParams({ inst, begin, end, ...(refresh ? { refresh: '1' } : {}) })}`, signal)
}

export function commodities(signal?: AbortSignal): Promise<{ results: CommodityQuote[]; failed: string[] | null }> {
  return request('/api/v1/commodity', signal)
}

export function commodityHistory(id: string, begin: string, end: string, signal?: AbortSignal): Promise<CommodityHistory> {
  return request(`/api/v1/commodity/history?${new URLSearchParams({ id, begin, end })}`, signal)
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

export function annualReports(code: string, startYear: number, endYear: number, page: number, type: string, signal?: AbortSignal): Promise<AnnualReportsPage> {
  return request(`/api/v1/annual-reports?${new URLSearchParams({ code, start_year: String(startYear), end_year: String(endYear), page: String(page), type })}`, signal)
}

export function downloadAnnualReport(code: string, year: number, url: string, type: string, signal?: AbortSignal): Promise<SavedDownload> {
  return request('/api/v1/annual-reports/download', signal, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code, year: String(year), url, type }),
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

export async function loadWatchlist(signal?: AbortSignal): Promise<BasicSecurity[]> {
  const data = await request<{ results: BasicSecurity[] }>('/api/v1/watchlist', signal)
  return data.results
}
export async function changeWatchlist(action: 'add' | 'remove', security: BasicSecurity): Promise<BasicSecurity[]> {
  const data = await request<{ results: BasicSecurity[] }>('/api/v1/watchlist', undefined, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(action === 'add' ? { action, security } : { action, code: security.ex_code }),
  })
  return data.results
}

export function versionDetails(signal?: AbortSignal): Promise<import('./types').VersionDetails> {
  return request('/api/v1/version', signal)
}
export function checkUpdates(signal?: AbortSignal): Promise<import('./types').UpdateDetails> {
  return request('/api/v1/updates', signal)
}

export function macroCatalog(signal?: AbortSignal): Promise<MacroCatalog> {
  return request('/api/v1/macro/catalog', signal)
}

export function macroData(query: Record<string, string>, signal?: AbortSignal): Promise<MacroDataset> {
  return request(`/api/v1/macro/data?${new URLSearchParams(query)}`, signal)
}

export function fxOptions(signal?: AbortSignal): Promise<{ cross_pairs: Record<string, string[]> }> {
  return request('/api/v1/fx/options', signal)
}

export function fxData(query: Record<string, string>, signal?: AbortSignal): Promise<FxDataset | FxSpread> {
  return request(`/api/v1/fx/data?${new URLSearchParams(query)}`, signal)
}

export function ipoAnnouncements(page: number, since: string, until: string, signal?: AbortSignal): Promise<PageResult<Announcement>> {
  return request(`/api/v1/ipo/announcements?${new URLSearchParams({ page: String(page), since, until })}`, signal)
}

export function valuation(query: Record<string, string>, signal?: AbortSignal): Promise<ValuationResult> {
  return request(`/api/v1/valuation?${new URLSearchParams(query)}`, signal)
}

export function strategy(query: Record<string, string>, signal?: AbortSignal): Promise<StrategyResult> {
  return request(`/api/v1/strategy?${new URLSearchParams(query)}`, signal)
}

export function filings(code: string, type: string, page: number, signal?: AbortSignal): Promise<SECFilings> {
  return request(`/api/v1/filings?${new URLSearchParams({ code, type, page: String(page) })}`, signal)
}

export function fundOverview(code: string, signal?: AbortSignal): Promise<FundOverview> {
  return request(`/api/v1/fund/overview?${new URLSearchParams({ code })}`, signal)
}

export function hkCompany(code: string, signal?: AbortSignal): Promise<HKCompanyData> {
  return request(`/api/v1/hk/company?${new URLSearchParams({ code })}`, signal)
}

export function usCompany(code: string, signal?: AbortSignal): Promise<USCompanyData> {
  return request(`/api/v1/us/company?${new URLSearchParams({ code })}`, signal)
}

export function usDividends(code: string, signal?: AbortSignal): Promise<USDividends> {
  return request(`/api/v1/us/dividends?${new URLSearchParams({ code })}`, signal)
}

export function usFinancials(code: string, statement: string, period: string, signal?: AbortSignal): Promise<USFinancialsData> {
  return request(`/api/v1/us/financials?${new URLSearchParams({ code, statement, period })}`, signal)
}
