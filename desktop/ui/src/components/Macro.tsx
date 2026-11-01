import { useCallback, useMemo, useState, type FormEvent } from 'react'
import { macroCatalog, macroData } from '../api'
import { useResource } from '../hooks/useResource'
import type { MacroCatalog, MacroDataset, MacroIndicator, MacroObservation, MacroSeries } from '../types'
import Bond from './Bond'
import BondChart from './BondChart'
import PageActions from './PageActions'
import Pagination from './Pagination'
import ResourceState from './ResourceState'

const regions = [{ id: 'cn', title: '中国' }, { id: 'us', title: '美国' }] as const

const measureLabels: Record<string, string> = {
  level: '水平', ytd_level: '年初累计', yoy_percent: '同比', mom_percent: '环比', qoq_percent: '环比', ytd_yoy_percent: '累计同比',
  qoq_annualized_percent: '环比年化', yoy_index: '同比指数', mom_index: '环比指数', qoq_index: '环比指数', monthly_flow: '当月增量', ytd_flow: '年初累计增量',
  diffusion_index: '扩散指数', rate: '利率', change: '变化', yield: '收益率', yield_to_maturity: '到期收益率', par_yield: '平价收益率',
  lower_bound: '下限', upper_bound: '上限', lower_change: '下限变化', upper_change: '上限变化', change_lower: '幅度下界', change_upper: '幅度上界',
  daily: '当日', month_to_date: '月累计', fiscal_year_to_date: '财年累计',
  index: '指数', count: '数量', yoy_change: '同比变化', mom_change: '环比变化', qoq_change: '环比变化', monthly_change: '月度变化', weekly_change: '周变化', mom_percentage_points: '较上月变化', qoq_percentage_points: '较上季变化', yoy_percentage_points: '较上年变化',
  previous_month_change: '较上月变化', previous_observation_change: '较前次变化', adjustment_change: '调整幅度', four_week_average: '四周均值', monthly_flow_derived: '当月增量（差额近似）', reported_level: '报告值', reported_yoy_percent: '报告同比', ytd_real_yoy_percent: '累计实际同比', ytd_yoy_index: '累计同比指数',
  receipts: '收入', outlays: '支出', surplus_or_deficit: '盈余/赤字', deficit_positive: '赤字（正数）', fytd_receipts: '财年累计收入', fytd_outlays: '财年累计支出', fytd_surplus_or_deficit: '财年累计盈余/赤字', fytd_deficit_positive: '财年累计赤字（正数）',
  issues_total: '发行合计', redemptions_total: '赎回合计', net_recorded_issuance: '净记账发行', awarded_rate: '中标利率', offering: '发行额', price_per_100: '百元面值价格', total_accepted: '总中标', competitive_accepted: '竞争性中标', noncompetitive_accepted: '非竞争性中标', soma_accepted: 'SOMA', fima_noncompetitive_accepted: 'FIMA非竞争性', direct_bidder_accepted: '直接投标人', indirect_bidder_accepted: '间接投标人', primary_dealer_accepted: '一级交易商',
}
const unitLabels: Record<string, string> = {
  percent: '%', percentage_points: '百分点', basis_points: '基点', index: '指数', CNY_100_million: '亿元', USD_100_million: '亿美元',
  USD_million: '百万美元', USD_billion: '十亿美元', USD: '美元', USD_per_hour: '美元/小时', USD_per_100_par: '美元/百元面值', CNY_per_person: '元/人',
  housing_units: '套', housing_units_thousand: '千套', hours_per_week: '小时/周', claims: '件', persons: '人', jobs_thousand: '千个', square_meters_10000: '万平方米',
}
const missingLabels: Record<string, string> = {
  source_value_missing: '官方未公布', current_value_missing: '当期值缺失', source_series_unavailable: '当期目录没有此序列', report_not_found: '未检索到报告',
  not_published: '未公布', not_surveyed: '未调查', january_exempt: '1月免报', publication_paused: '暂停发布', definition_suspended: '列报暂停',
  baseline_missing: '计算基期缺失', previous_observation_missing: '前值缺失', window_incomplete: '累计窗口不完整', methodology_break: '口径切换，不计算',
  nonpositive_baseline: '基期非正，不计算', zero_baseline: '基期为零，不计算', calculation_unavailable: '无法计算', official_not_computable: '官方不可计算',
  level_ratio_not_break_adjusted: '未调整断点，不计算', level_ratio_not_comparable_scope: '范围不可比，不计算', not_applicable: '不适用',
}

const unitText = (unit: string) => unitLabels[unit] ?? unit
// 部分序列的定义是来源代码（GDPC1、offering_amt），这时只显示口径名称，代码留在序列ID里。
function seriesName(series: MacroSeries) {
  const measure = measureLabels[series.measure] ?? series.measure
  if (!/[\s\u4e00-\u9fff]/.test(series.definition)) return measure
  return series.definition.includes(measure) ? series.definition : `${series.definition} · ${measure}`
}
const valueText = (value: number | null) => value === null ? '—' : value.toLocaleString('zh-CN', { maximumFractionDigits: 6 })
const tickText = (value: number) => value.toLocaleString('zh-CN', { notation: 'compact', maximumFractionDigits: 2 })
const statusText = (item: MacroObservation) => item.value !== null ? '' : (item.missing_reason && missingLabels[item.missing_reason]) || item.missing_reason || '缺失'

function eventText(data: MacroDataset, id?: string) {
  if (!id) return ''
  const event = [...(data.events ?? []), ...(data.policy_events ?? [])].find(item => item.id === id)
  if (!event) return id
  return [event.scope, event.cusip, event.security_type, event.security_term].filter(Boolean).join(' ')
}

// 月份用文本框：WKWebView 不支持 type="month"。命令使用 YYYYMM、YYYYQ1、YYYYMMDD。
const inputType = { month: 'text', date: 'date', quarter: 'text' } as const
const placeholders = { month: '2026-01', date: undefined, quarter: '2026Q1' } as const
const periodFormats = {
  month: { pattern: /^\d{4}(0[1-9]|1[0-2])$/, message: '月份格式为 2026-01' },
  quarter: { pattern: /^\d{4}Q[1-4]$/, message: '季度格式为 2026Q1' },
  date: { pattern: /^\d{8}$/, message: '日期格式为 2026-01-31' },
} as const
function toInput(period: MacroIndicator['period'], value: string) {
  if (period === 'month' && value.length === 6) return `${value.slice(0, 4)}-${value.slice(4)}`
  if (period === 'date' && value.length === 8) return `${value.slice(0, 4)}-${value.slice(4, 6)}-${value.slice(6)}`
  return value
}
const toQuery = (value: string) => value.trim().replace(/-/g, '').toUpperCase()

// 从 anchor（YYYYMMDD）或今天往前推算开始所属期。
function periodBefore(period: MacroIndicator['period'], days: number, anchor?: string) {
  const end = anchor ? Date.UTC(+anchor.slice(0, 4), +anchor.slice(4, 6) - 1, +anchor.slice(6, 8)) : Date.now()
  const date = new Date(end - days * 86400000)
  const iso = date.toISOString().slice(0, 10)
  if (period === 'date') return toQuery(iso)
  if (period === 'month') return iso.slice(0, 7).replace('-', '')
  return `${iso.slice(0, 4)}Q${Math.floor(date.getUTCMonth() / 3) + 1}`
}
const quickRanges = {
  date: [[30, '近1个月'], [90, '近3个月'], [365, '近1年']],
  month: [[365, '近1年'], [1095, '近3年'], [1825, '近5年']],
  quarter: [[730, '近2年'], [1825, '近5年'], [3650, '近10年']],
} as const

// 逐券拍卖、准备金率这类事件之间不是连续序列，同一日期有多条记录时也无法连线。
function isEventSeries(data: MacroDataset, rows: MacroObservation[]) {
  if (data.events?.length || data.policy_events?.length) return true
  return new Set(rows.map(item => item.period)).size < rows.length
}

function SeriesHistory({ data, series }: { data: MacroDataset; series: MacroSeries }) {
  const [page, setPage] = useState(1)
  const rows = useMemo(() => data.observations.filter(item => item.series_id === series.id).sort((a, b) => a.period.localeCompare(b.period)), [data, series.id])
  const unit = unitText(series.unit)
  const hasEvents = rows.some(item => item.event_id)
  const source = rows[rows.length - 1]?.source
  const pageSize = 30, shown = [...rows].reverse().slice((page - 1) * pageSize, page * pageSize)
  return <section className="page-panel" aria-label="序列走势与明细">
    <div className="section-caption"><span>{seriesName(series)}（{unit}）</span><span>{series.value_origin === 'official' ? '官方值' : '计算值'} · {series.id}</span></div>
    {series.formula && <p className="data-note">计算方式：{series.formula}</p>}
    {series.coverage && <p className="data-note">范围：{series.coverage}</p>}
    {isEventSeries(data, rows)
      ? <p className="data-note">这是事件型数据，各事件不是连续序列，因此不绘制走势，请查看下方明细。</p>
      : <BondChart key={series.id} title={seriesName(series)} points={rows.map(item => ({ label: item.period, value: item.value }))} format={value => `${valueText(value)} ${unit}`} tick={tickText} minPadding={0} emptyText="此区间暂无可用数据" valueLabel={`单位${unit}`} axisLabel="所属期" />}
    <div className="table-scroll"><table className="data-table"><thead><tr><th>所属期</th>{hasEvents && <th>事件</th>}<th>数值</th><th>状态</th><th>公布时间</th><th>说明</th></tr></thead>
      <tbody>{shown.map((item, index) => <tr key={`${item.period}-${item.event_id ?? index}`}><td>{item.period}</td>{hasEvents && <td>{eventText(data, item.event_id)}</td>}<td>{valueText(item.value)}</td><td>{statusText(item)}</td><td>{item.published_at ?? item.published_date ?? '—'}</td><td>{item.note || ''}</td></tr>)}</tbody></table></div>
    <Pagination page={page} total={rows.length} hasMore={page * pageSize < rows.length} loading={false} onPage={setPage} />
    {source && <p className="data-note">来源：{source}</p>}
  </section>
}

function DatasetView({ data, selectedID, onSelect, filter, onFilter }: { data: MacroDataset; selectedID: string; onSelect: (id: string) => void; filter: string; onFilter: (value: string) => void }) {
  // 同一最新所属期有多条有值记录时，不任取其一作为最新值。
  const latest = useMemo(() => {
    const result = new Map<string, { item: MacroObservation; count: number }>()
    for (const item of data.observations) {
      if (item.value === null) continue
      const current = result.get(item.series_id)
      if (!current || item.period > current.item.period) result.set(item.series_id, { item, count: 1 })
      else if (item.period === current.item.period) current.count++
    }
    return result
  }, [data])
  // 查询窗口内没有观测的序列定义只说明口径，不进入列表。
  const withData = useMemo(() => data.series.filter(series => data.observations.some(item => item.series_id === series.id)), [data])
  const keyword = filter.trim().toLowerCase()
  const visible = keyword ? withData.filter(series => `${seriesName(series)} ${series.id}`.toLowerCase().includes(keyword)) : withData
  const selected = withData.find(series => series.id === selectedID) ?? withData[0]
  if (!withData.length) return <section className="page-panel"><div className="empty-state">所选区间没有数据，请调整日期。不代表数值为零。</div></section>
  return <>
    <section className="page-panel" aria-label="序列列表">
      <div className="section-toolbar"><h2>序列</h2><span className="data-note">{data.begin} 至 {data.end} · {withData.length} 条序列 · {data.observations.length} 条观测</span>
        {withData.length > 8 && <input aria-label="筛选序列" placeholder="筛选序列名称" value={filter} onChange={event => onFilter(event.target.value)} />}</div>
      <div className="table-scroll macro-series"><table className="data-table"><thead><tr><th>序列</th><th>最新值</th><th>单位</th><th>所属期</th><th>口径</th></tr></thead>
        <tbody>{visible.map(series => { const entry = latest.get(series.id); return <tr key={series.id} aria-selected={series.id === selected?.id}>
          <td><button className="text-link" aria-pressed={series.id === selected?.id} title={series.id} onClick={() => onSelect(series.id)}>{seriesName(series)}</button></td>
          <td>{!entry ? '—' : entry.count > 1 ? '多个事件' : valueText(entry.item.value)}</td><td>{unitText(series.unit)}</td><td>{entry?.item.period ?? '—'}</td><td>{series.value_origin === 'official' ? '官方' : '计算'}</td></tr> })}</tbody></table></div>
      {!visible.length && <div className="empty-state">没有匹配的序列</div>}
    </section>
    {selected && <SeriesHistory key={selected.id} data={data} series={selected} />}
  </>
}

interface Query { begin: string; end: string; options: Record<string, string>; refresh: boolean }

function IndicatorView({ indicator }: { indicator: MacroIndicator }) {
  const [query, setQuery] = useState<Query>({ begin: indicator.default_begin ?? '', end: '', options: {}, refresh: false })
  const [draft, setDraft] = useState({ begin: toInput(indicator.period, indicator.default_begin ?? ''), end: '' })
  const [validation, setValidation] = useState('')
  // 查询期间数据会被清空，选中序列和筛选放在这一层，换指标时才重置。
  const [selectedID, setSelectedID] = useState('')
  const [filter, setFilter] = useState('')
  const load = useCallback(async (signal: AbortSignal) => {
    const params: Record<string, string> = { region: indicator.region, command: indicator.command, ...query.options }
    if (query.begin) params.begin = query.begin
    if (query.end) params.end = query.end
    if (query.refresh) params.refresh = 'true'
    const data = await macroData(params, signal)
    // 只回填仍为空的输入框，不覆盖用户正在输入的日期。
    setDraft(current => ({ begin: current.begin || toInput(indicator.period, data.begin), end: current.end || toInput(indicator.period, data.end) }))
    return data
  }, [indicator, query])
  const resource = useResource(load)
  // 提交当前输入的日期和选项；格式不对时不发请求。
  const apply = (options: Record<string, string>) => {
    const begin = toQuery(draft.begin), end = toQuery(draft.end)
    const format = periodFormats[indicator.period]
    if ((begin && !format.pattern.test(begin)) || (end && !format.pattern.test(end))) { setValidation(format.message); return }
    if (begin && end && begin > end) { setValidation('开始不能晚于结束'); return }
    setValidation('')
    setQuery({ begin, end, options, refresh: false })
  }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    apply(query.options)
  }
  const quick = (days: number) => {
    const anchor = indicator.latest_end
    const begin = periodBefore(indicator.period, days, anchor), end = anchor ?? ''
    setValidation('')
    setDraft({ begin: toInput(indicator.period, begin), end: toInput(indicator.period, end) })
    setQuery({ ...query, begin, end, refresh: false })
  }
  const inputProps = { type: inputType[indicator.period], placeholder: placeholders[indicator.period] }
  return <div className="bond-page">
    <section className="page-panel" aria-label={`${indicator.title}查询`}>
      <form className="section-toolbar bond-range-form" onSubmit={submit}>
        <label>开始<input aria-label="开始" {...inputProps} value={draft.begin} onChange={event => setDraft({ ...draft, begin: event.target.value })} /></label>
        <label>结束<input aria-label="结束" {...inputProps} value={draft.end} onChange={event => setDraft({ ...draft, end: event.target.value })} /></label>
        {indicator.options?.map(option => option.values
          ? <label key={option.flag}>{option.label}<select aria-label={option.label} value={query.options[option.flag] ?? option.values[0].value} onChange={event => apply({ ...query.options, [option.flag]: event.target.value })}>{option.values.map(item => <option key={item.value} value={item.value}>{item.label}</option>)}</select></label>
          : <label key={option.flag}><input type="checkbox" checked={query.options[option.flag] === 'true'} onChange={event => apply({ ...query.options, [option.flag]: event.target.checked ? 'true' : '' })} />{option.label}</label>)}
        <button className="primary-button" type="submit">查询</button>
        <PageActions><button type="button" disabled={resource.loading} onClick={() => { setValidation(''); setQuery({ ...query, refresh: true }) }}>刷新</button></PageActions>
      </form>
      <div className="bond-quick-ranges"><span className="data-note">快捷区间</span>{quickRanges[indicator.period].map(([days, label]) => <button key={days} type="button" onClick={() => quick(days)}>{label}</button>)}</div>
      {validation && <div className="error" role="alert">{validation}</div>}
    </section>
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && <DatasetView data={resource.data} selectedID={selectedID} onSelect={setSelectedID} filter={filter} onFilter={setFilter} />}
  </div>
}

function MacroBrowser({ catalog }: { catalog: MacroCatalog }) {
  const [region, setRegion] = useState<'cn' | 'us'>('cn')
  const [selected, setSelected] = useState<Record<string, string>>({ cn: 'gdp', us: 'gdp' })
  const items = catalog.indicators.filter(item => item.region === region)
  const indicator = items.find(item => item.command === selected[region]) ?? items[0]
  return <div className="macro-page">
    <nav className="page-panel macro-menu" aria-label="宏观指标">
      <div className="segmented" role="group" aria-label="国家">{regions.map(item => <button key={item.id} aria-pressed={region === item.id} onClick={() => setRegion(item.id)}>{item.title}</button>)}</div>
      {catalog.categories.map(category => { const list = items.filter(item => item.category === category.id); return <div key={category.id} className="macro-category">
        <h2>{category.title}</h2>
        {list.map(item => <button key={item.command} aria-current={item === indicator ? 'true' : undefined} onClick={() => setSelected({ ...selected, [region]: item.command })}>{item.title}</button>)}
      </div> })}
    </nav>
    <div className="macro-content">
      <div className="section-caption"><span>{regions.find(item => item.id === region)?.title} · {indicator.title}</span><span>sec macro {region} {indicator.command}</span></div>
      {region === 'us' && indicator.command === 'treasury' ? <Bond /> : <IndicatorView key={`${region}/${indicator.command}`} indicator={indicator} />}
    </div>
  </div>
}

export default function Macro() {
  const resource = useResource(macroCatalog)
  return <>
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && <MacroBrowser catalog={resource.data} />}
  </>
}
