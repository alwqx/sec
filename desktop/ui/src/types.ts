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
  turnover?: number // 成交额，币种与价格一致
}

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
