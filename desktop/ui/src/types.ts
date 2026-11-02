export interface BasicSecurity {
  name: string
  security_type: string
  code: string
  ex_code: string
  exchange: string
}

export interface SecurityQuote {
  trade_date: string
  code: string
  ex_code: string
  name: string
  current: number
  open: number
  yclose: number
  high: number
  low: number
  volume: number // 成交金额（元）
  turnover: number // 成交数量（股）
  time: string
}

export interface Company {
  name: string
  en_name: string
  history_name: string
  listing_date: string
  listing_price: number
  website: string
  register_address: string
  business_address: string
  main_business: string
}

export interface Dividend {
  public_date: string
  record_date: string
  dividend_date: string
  shares: number
  add_shares: number
  bonus: number
}

export interface WorkspaceSettings {
  download_directory: string
  refresh_seconds: number
  theme: 'dark' | 'light' | 'system'
}

export interface WorkspaceState {
  watchlist: BasicSecurity[]
  settings: WorkspaceSettings
}

export interface PageResult<T> {
  results: T[]
  total: number
  has_more: boolean
  truncated?: boolean
}

export interface IPOListing {
  code: string
  name: string
  listing_date: string
}

export interface Announcement {
  id: string
  title: string
  date: string
  code: string
  name: string
  url: string
}

export interface FinancialReport {
  columns: { key: string; label: string }[]
  results: { date: string; fields: Record<string, unknown> }[]
}

export interface MetalQuote {
  date: string
  open: number
  close: number
  high: number
  low: number
  yclose: number | null
  change: number | null
  change_rate: number | null // 百分比
}

export interface MetalData {
  name: string
  unit: string
  begin: string
  end: string
  latest: MetalQuote | null
  results: MetalQuote[]
}

export interface HistoryQuote {
  date: string
  open: number
  close: number
  high: number
  low: number
  volume?: number // 股/份；沪深成交量已由 provider 从手转换
  turnover?: number | null // 成交额，币种与价格一致；备用数据源没有时为 null
}

export interface HistoryOverlay { label: string; kind: 'ma' | 'boll'; values: (number | null)[] }

export interface BondDay {
  date: string
  yields: Record<string, number | null>
  previous_10y: number | null
  change_bp: number | null
}

export interface BondData {
  begin: string
  end: string
  latest: BondDay | null
  results: BondDay[]
  tenors: { key: string; label: string; months: number }[]
}

export interface CompanyMetrics {
  price_is_yclose: boolean
  code: string
  industry: string
  pe_ttm: number | null
  pb: number | null
  market_cap: number | null
  float_market_cap: number | null
  currency: 'CNY'
  trade_date: string
  time: string
}

export interface SavedDownload {
  filename: string
  path: string
  size: number
}

export interface AnnualReport {
  type: string
  id: string
  title: string
  date: string
  year: number
  url: string
}

export interface AnnualReportsPage {
  results: AnnualReport[]
  page: number
  has_more: boolean
}

export interface ProspectusDocument {
  id: string
  title: string
  date: string
  url: string
}

export interface ProspectusPage {
  code: string
  name: string
  results: ProspectusDocument[]
  page: number
  has_more: boolean
}

export interface VersionDetails {
  version: string
  build_time: string
  git_commit: string
  os: string
  arch: string
  release_url: string
}
export interface UpdateDetails {
  latest_version: string
  status: 'available' | 'current' | 'ahead' | 'unknown'
  release_url: string
  asset: { name: string; browser_download_url: string; size: number } | null
}

export interface MacroOption { flag: string; label: string; values?: { value: string; label: string }[] }
export interface MacroIndicator { region: 'cn' | 'us'; command: string; title: string; category: string; period: 'month' | 'quarter' | 'date'; default_begin?: string; latest_end?: string; options?: MacroOption[] }
export interface MacroCatalog { categories: { id: string; title: string }[]; indicators: MacroIndicator[] }
export interface MacroSeries { id: string; measure: string; unit: string; frequency: string; value_origin: string; definition: string; formula: string | null; coverage?: string }
export interface MacroObservation {
  series_id: string; period: string; value: number | null; status: string; event_id?: string; missing_reason?: string | null
  published_date?: string | null; published_at?: string | null; note?: string; source?: string | null; raw_value?: string | null
}
export interface MacroDataset { begin: string; end: string; series: MacroSeries[]; observations: MacroObservation[]; events?: Record<string, unknown>[]; policy_events?: Record<string, unknown>[] }

export interface FxObservation {
  date: string; pair: string; market: string; value: number | null; bid?: number | null; ask?: number | null; open?: number | null; high?: number | null; low?: number | null; close?: number | null
  observed_at?: string; source?: string; note?: string; is_derived?: boolean; formula?: string; inputs?: FxObservation[]; alignment?: FxAlignment
}
export interface FxAlignment { status: string; skew_seconds?: number; time_start?: string; time_end?: string; note?: string }
export interface FxDataset { provider: string; price_type: string; frequency: string; timezone: string; observation_time?: string; begin?: string; end?: string; observations: FxObservation[] | null; notes?: string[] }
export interface FxSpread {
  provider: string; as_of: string; is_derived: boolean; formula: string; inputs: FxObservation[] | null; alignment: FxAlignment; notes?: string[]
  difference_rmb_per_usd: number | null; pips: number | null; relative_percent: number | null; relative_bps: number | null; implied_cnh_cny: number | null
}

export interface StrategySeries { label: string; panel: 'price' | 'indicator'; style: 'line' | 'bar'; values: (number | null)[] }
export interface StrategySignal { date: string; type: 'buy' | 'sell'; price: number; reason: string }
export interface StrategyResult { source_note?: string; params: Record<string, number>; levels: number[] | null; dates: string[]; closes: number[]; series: StrategySeries[]; signals: StrategySignal[]; required: number; insufficient: boolean }

export interface ValuationHistory { since: string; samples: number; min: number; median: number; max: number; percentile: number | null; short: boolean }
export interface ValuationScenario { label: string; multiple: number; price: number; change: number }
export interface ValuationMultiple { current: number | null; raw: number | null; history: ValuationHistory | null; series: (number | null)[]; scenarios: ValuationScenario[] }
export interface ValuationAnnual { year: string; eps: number | null; bps: number | null; roe: number | null; revenue: number | null; profit: number | null }
export interface ValuationDCF {
  applicable: boolean; note: string; warning: string; growth_source: string
  params: { growth: number | null; terminal_growth: number; wacc: number; margin_of_safety: number }
  base_cash: number | null; growth_pv: number | null; terminal_pv: number | null; terminal_share: number | null; value: number | null; per_share: number | null; fair_price: number | null; upside: number | null
}
export interface ValuationResult {
  code: string; name: string; industry: string; trade_date: string; price: number; market_cap: number; shares: number
  pe: ValuationMultiple; pb: ValuationMultiple; ps: ValuationMultiple; dates: string[]
  eps: number | null; bvps: number | null; profit_ttm: number | null; revenue_ttm: number | null; roe: number | null; roe_year: string
  growth: { rate: number | null; from_year: string; to_year: string; note: string }
  peg: number | null; graham: number | null; graham_margin: number | null
  annuals: ValuationAnnual[]; dcf: ValuationDCF; signals: { name: string; level: 'low' | 'neutral' | 'high'; rule: string }[]; assessment: string; disclaimer: string
}
