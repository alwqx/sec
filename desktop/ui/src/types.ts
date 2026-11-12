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
  volume: number | null // 成交金额（元）；基金净值没有成交，为 null
  turnover: number | null // 成交数量（股）；基金净值没有成交，为 null
  time: string
  timezone: string // trade_date 和 time 所在的交易所时区，美股是 America/New_York
  nav?: boolean // 价格是基金单位净值：trade_date 是净值日期，yclose 是上一日净值，没有时间、开高低和成交
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
  sidebar_hidden?: boolean // 左侧菜单是否隐藏，旧的工作区文件没有这一项（按展开处理）
  sec_edgar_email?: string // 保存在和 CLI 共用的 ~/.sec/config.json
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
  // 链接是巨潮 PDF 原文时才能摘要。
  summarizable?: boolean
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

export interface MetalInstrument { id: string; name: string; unit: string; alias: string }

export interface MetalData {
  inst: string
  name: string
  description: string
  unit: string
  instruments: MetalInstrument[]
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

// AISettings 是和 CLI 共用的模型配置（GET /api/v1/ai），Key 只有来源和末 4 位。
export interface AIProfileView {
  name: string
  default: boolean
  provider: string
  provider_name: string
  api: string
  base_url: string
  model: string
  api_key_env?: string
  key: string
  key_stored: boolean // 有没有加密保存的 Key
  local: boolean
  thinking?: string // off 表示关闭思考，没有时用厂商默认
}
export interface AIProvider {
  id: string
  name: string
  api: string // 默认协议，自定义厂商为空
  base_url: string // 官方地址，自定义厂商为空
  key_env?: string // 没有设置 Key 时读取的环境变量
}
export interface AISettings {
  enabled: boolean
  default: string
  profiles: AIProfileView[]
  providers: AIProvider[]
  thinking_off_providers: string[] // 能关闭思考的厂商 id
  invalid?: string // ai 段无效的原因
  notice?: string // 修改后的提示，只在修改接口的返回里有
}
// AIProbeResult 是连接测试的结果（POST /api/v1/ai/profiles/{name}/test），和 sec config ai test --json 一致。
export interface AIProbeResult {
  name: string
  provider: string
  api: string
  base_url: string
  model: string
  key: string
  local: boolean
  ok: boolean
  error?: string
  hint?: string
  sent: boolean // 是否真的发出了请求
  latency_ms: number
  warning?: string
  reply_model?: string
  reply?: string
  usage: { input_tokens: number; output_tokens: number }
  tools?: 'supported' | 'not_called' | 'error' | 'unknown'
  tools_detail?: string
}
// ValuationExplanation 与 llm.Explanation 一致，核对位置按 Unicode 字符计。
export interface ValuationExplanation {
  ex_code: string
  name: string
  as_of: string
  text: string
  disclaimer: string
  check: {
    numbers: number
    unsourced: { text: string; start: number; end: number }[]
    advice_terms: { text: string; start: number; end: number }[]
  }
  truncated: boolean
  truncation_hint?: string // 被截断的原因和建议，如关闭思考，接在「解读不完整」后面
  model: string
  profile: string
  prompt_version: string
  cached: boolean
  created_at: string
}

export interface AnnouncementSummary {
  document_id: string
  source_url: string
  as_of: string
  page_count: number
  sections: { text: string; pages: number[] }[]
  text: string
  notes: string[]
  check: ValuationExplanation['check']
  disclaimer: string
  model: string
  profile: string
  prompt_version: string
  cached: boolean
  created_at: string
}

export type ValuationExplanationMeta = Pick<ValuationExplanation, 'ex_code' | 'name' | 'as_of' | 'model' | 'profile' | 'disclaimer' | 'cached'>
export type ExplanationStreamEvent =
  | { type: 'status'; text: string }
  | { type: 'delta'; text: string }
  | { type: 'meta'; meta: ValuationExplanationMeta }
  | { type: 'done'; result: ValuationExplanation }

// AIProfileInput 是新增和编辑提交的配置；协议和地址为空表示用厂商的默认值。不含 Key。
export interface AIProfileInput {
  name?: string
  provider: string
  api: string
  base_url: string
  model: string
  api_key_env: string
  thinking: string // off 关闭思考，空为厂商默认
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
export interface BacktestPerformance { total_return: number; annualized: number | null; max_drawdown: number; drawdown_start: string; drawdown_end: string; final_equity: number }
export interface BacktestResult {
  config: { cash: number; commission: number; min_commission: number; stamp_tax: number; slippage: number }
  begin: string; end: string; strategy: BacktestPerformance; benchmark: BacktestPerformance
  round_trips: number; win_rate: number | null; total_fees: number; exposure: number; open_position: boolean
  trades: { signal_date: string; date: string; side: 'buy' | 'sell'; price: number; shares: number; amount: number; fee: number; reason: string; profit: number | null }[]
  events: { date: string; side: 'buy' | 'sell'; reason: string }[]
  curve: { date: string; equity: number; benchmark: number; holding: boolean }[]
}
export interface StrategyResult { source_note?: string; params: Record<string, number>; levels: number[] | null; dates: string[]; closes: number[]; series: StrategySeries[]; signals: StrategySignal[]; required: number; insufficient: boolean; backtest: BacktestResult | null; backtest_note: string }

export interface ValuationHistory { since: string; samples: number; min: number; median: number; max: number; percentile: number | null; short: boolean }
export interface ValuationScenario { label: string; multiple: number; price: number; change: number }
export interface ValuationMultiple { current: number | null; raw: number | null; history: ValuationHistory | null; series: (number | null)[]; scenarios: ValuationScenario[] }
export interface ValuationAnnual { year: string; eps: number | null; bps: number | null; roe: number | null; revenue: number | null; profit: number | null }
export interface ValuationDCF {
  applicable: boolean; note: string; warning: string; growth_source: string
  params: { growth: number | null; terminal_growth: number; wacc: number; margin_of_safety: number }
  base_cash: number | null; growth_pv: number | null; terminal_pv: number | null; terminal_share: number | null; value: number | null; per_share: number | null; fair_price: number | null; upside: number | null
}
export interface ValuationPeerMetric {
  current: number | null; samples: number; excluded: number; short: boolean; median: number | null; rank: number | null; percentile: number | null; premium: number | null
}
export interface ValuationPeer { code: string; name: string; self: boolean; market_cap: number | null; pe: number | null; pb: number | null; ps: number | null }
export interface ValuationPeers { board: string; date: string; pe: ValuationPeerMetric; pb: ValuationPeerMetric; ps: ValuationPeerMetric; items: ValuationPeer[] }
export interface ValuationDividends {
  yield: number | null; per_share: number; fiscal_year: string; average_5y: number | null; consecutive_years: number; history: ValuationHistory | null
  years: { year: string; per_share: number; eps: number | null; payout: number | null }[]
  pending: { report: string; progress: string; plan: string; per_share: number | null; ex_date: string }[]
  mismatches: { ex_date: string; sina: number; east_money: number | null }[]; mismatch_checked: boolean
}
export interface ValuationResult {
  code: string; currency: 'CNY' | 'USD'; name: string; industry: string; trade_date: string; price: number; market_cap: number; shares: number
  pe: ValuationMultiple; pb: ValuationMultiple; ps: ValuationMultiple; dates: string[]
  eps: number | null; bvps: number | null; profit_ttm: number | null; revenue_ttm: number | null; roe: number | null; roe_year: string
  growth: { rate: number | null; from_year: string; to_year: string; note: string }
  peg: number | null; graham: number | null; graham_margin: number | null
  annuals: ValuationAnnual[]; peers: ValuationPeers | null; peers_note: string; dividends: ValuationDividends | null; dividends_note: string; shares_note: string; dcf: ValuationDCF; signals: { name: string; level: 'low' | 'neutral' | 'high'; rule: string }[]; assessment: string; disclaimer: string
}

export interface FinancialItem { label: string; value: number }
export interface FinancialBridge { start: FinancialItem | null; steps: { items: FinancialItem[]; total: FinancialItem }[] }
export interface FinancialMetrics {
  revenue: number | null; net_profit: number | null; parent_net_profit: number | null; deduct_net_profit: number | null
  gross_margin: number | null; net_margin: number | null; sale_ratio: number | null; manage_ratio: number | null; research_ratio: number | null; finance_ratio: number | null
  cost_income_ratio: number | null; debt_ratio: number | null; operating_cash: number | null; investing_cash: number | null; financing_cash: number | null; cash_to_profit: number | null
  sga_ratio?: number | null
}
// label 是美股的财年标注（比如 FY2026Q1），A 股没有、按日期标注。
export interface FinancialPeriod { date: string; label?: string; income: FinancialBridge | null; cash: FinancialBridge | null; assets: FinancialItem[] | null; metrics: FinancialMetrics }
// source 为 sec 时是美股（SEC XBRL），currency_name 是金额的币种；A 股两者都没有。
export interface FinancialCharts { code: string; company_type: 'general' | 'bank' | 'securities' | 'insurance'; quarterly: boolean; source?: 'sec'; currency_name?: string; periods: FinancialPeriod[] }

export interface CommodityInfo { id: string; name: string; exchange: string; unit: string; domestic: boolean }
// 涨跌相对昨日结算价，change_rate 为百分比
export interface CommodityQuote extends CommodityInfo { date: string; time: string; price: number; open: number; high: number; low: number; prev_settle: number | null; change: number | null; change_rate: number | null }
// 国内涨跌相对前一日结算价，国际相对前一日收盘价；settle 只有国内品种有
// basis 为 settle 或 close：国内品种前一天没有结算价时用收盘价
export interface CommodityBar { date: string; open: number; high: number; low: number; close: number; volume: number; settle: number | null; yclose: number | null; basis: '' | 'settle' | 'close'; change: number | null; change_rate: number | null }
export interface CommodityHistory { commodity: CommodityInfo; begin: string; end: string; results: CommodityBar[] }

export interface SECFiling { form: string; filing_date: string; report_date: string; accession: string; items: string; description: string; size: number; url: string; index_url: string }
export interface SECProfile {
  cik: number; name: string; tickers: string[] | null; exchanges: string[] | null; industry: string; sic: string; fiscal_year_end: string
  category: string; state_of_incorporation: string; business_address: string; phone: string; website: string; former_names: { name: string; from: string; to: string }[] | null
}
export interface SECFilings { company: SECProfile; ticker: string; total: number; has_more: boolean; truncated: boolean; results: SECFiling[] }

export interface USQuote { name: string; price: number; change_rate: number; time: string; high_52w: number | null; low_52w: number | null; market_cap: number | null; eps: number | null; pe: number | null; shares: number | null }
// FundOverview 是基金概况（fund.Info）：天天基金 App 接口的净值和收益率，网页档案的全称、托管人和业绩比较基准等。
// 货币基金的 nav、acc_nav、day_change 为 null，改用 yield_10k（每万份收益，元）和 yield_7d（七日年化，%）；收益率单位都是 %。
export interface FundReturns { '1w': number | null; '1m': number | null; '3m': number | null; '6m': number | null; '1y': number | null; '2y': number | null; '3y': number | null; ytd: number | null; since_launch: number | null }
export interface FundOverview {
  ex_code: string; code: string; name: string; full_name: string; type: string; money_market: boolean; listed: boolean; risk_level: string
  company: string; custodian: string; managers: string; launch_date: string; launch_size: string; net_assets: number | null; net_assets_note: string
  management_fee: string; custody_fee: string; benchmark: string; tracking: string; objective: string
  nav: number | null; acc_nav: number | null; day_change: number | null; yield_10k: number | null; yield_7d: number | null; nav_date: string
  returns: FundReturns | null; notes: string[]
}
// HKCompanyData 是港股公司概况：东方财富公司资料和估值，新浪价格和市值（交易币种 currency，人民币柜台是 CNY）；
// equity 为 false 时是基金、信托、权证等，没有公司资料；取不到的值是 null 或空字符串，原因在 notes。
export interface HKCompanyData {
  ex_code: string; short_name: string; name: string; industry: string; listing_date: string; issue_price: number | null
  website: string; business_address: string; register_address: string; main_business: string; description: string
  price: number | null; price_is_yclose: boolean; pe_ttm: number | null; pb: number | null
  market_cap: number | null; float_market_cap: number | null; currency: string; equity: boolean; notes: string[]
}
export interface USCompanyData { info: { ex_code: string; ticker: string; company: SECProfile | null; quote: USQuote | null }; notes: string[] }
export interface USReport {
  statement: string; quarterly: boolean; standard: 'US GAAP' | 'IFRS'; currency: string; currency_name: string
  lines: { key: string; label: string; unit: string }[]
  periods: { end: string; start?: string; fiscal_year: number; fiscal_period: string; values: Record<string, number | null>; derived?: string[] }[]
}
export interface USFinancialsData { ticker: string; name: string; cik: number; report: USReport }

// USDividends 是美股按财年的分红表（usstock.Dividends），years 从新到旧；每股股息已按 splits 统一到当前股本，splits_unchecked 表示没有读到日线、没有调整。
export interface USDividendYear { fiscal_year: number; end: string; per_share: number | null; growth: number | null; paid: number | null; net_income: number | null; payout: number | null; buyback: number | null }
export interface USDividends {
  ticker: string; name: string; cik: number; currency: string; currency_name: string
  splits: { date: string; factor: number }[]; splits_unchecked: boolean; years: USDividendYear[]
}
