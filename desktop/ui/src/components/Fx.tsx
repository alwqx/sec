import { useCallback, useMemo, useState, type FormEvent } from 'react'
import { fxData, fxOptions } from '../api'
import { useResource } from '../hooks/useResource'
import type { FxDataset, FxObservation, FxSpread } from '../types'
import BondChart from './BondChart'
import PageActions from './PageActions'
import Pagination from './Pagination'
import ResourceState from './ResourceState'

const views = [
  { id: 'quote', title: '最新行情', hint: '新浪参考行情，可能延迟或为休市时的旧报价。' },
  { id: 'fixing', title: '人民币中间价', hint: '中国外汇交易中心公布的中间价，不是市场收盘价。' },
  { id: 'reference', title: '在岸参考汇率', hint: '固定时点的银行间参考汇率，不是中间价或 16:30 收盘价。' },
  { id: 'history', title: '历史日线', hint: '新浪日线与 FRED 纽约中午价分别保留各自口径，不混用。' },
  { id: 'cross', title: '交叉汇率', hint: '由同一来源的输入推导，不代表可实际成交的价格。' },
  { id: 'spread', title: '在岸／离岸价差', hint: '由新浪 USD/CNY、USD/CNH 报价推导，不是可实现的套利利润。' },
] as const
type View = typeof views[number]['id']

const sourceLabels: Record<string, string> = { sina: '新浪', fred: 'FRED 纽约中午价', cfets: 'CFETS 固定时点' }
const priceTypes: Record<string, string> = {
  'indicative-quote': '参考行情', 'provider-daily-bar': '来源日线', 'noon-buying-rate': '纽约中午买入价', 'central-parity': '人民币中间价',
  'onshore-reference': '在岸参考汇率', 'derived-cross-rate': '交叉汇率（推导值）',
}
const alignmentLabels: Record<string, string> = {
  synchronized: '时间一致', 'same-fixed-time': '同一固定时点', 'within-tolerance': '时间差在允许范围内（近似）', 'timestamp-mismatch': '时间差超限', 'stale-input': '输入已过期',
  'future-timestamp': '报价时间晚于当前', 'missing-input': '缺少输入', 'missing-timestamp': '缺少报价时间', 'missing-value': '输入缺少数值', 'unknown-timezone': '时区未知',
}
const atTimes = ['10:00', '11:00', '14:00', '15:00', '16:00']
const quickRanges = [[30, '近1个月'], [90, '近3个月'], [365, '近1年'], [1825, '近5年']] as const

const rate = (value: number | null | undefined) => value == null ? '—' : value.toLocaleString('zh-CN', { minimumFractionDigits: 4, maximumFractionDigits: 6 })
const number = (value: number | null | undefined, digits = 4) => value == null ? '—' : value.toLocaleString('zh-CN', { maximumFractionDigits: digits })
const timeText = (value?: string) => value ? value.replace('T', ' ').replace(/(\+|-)(\d\d:\d\d)$/, ' $1$2') : '—'
const toInput = (value: string) => value.length === 8 ? `${value.slice(0, 4)}-${value.slice(4, 6)}-${value.slice(6)}` : value
const toQuery = (value: string) => value.trim().replace(/-/g, '')
// 默认截至日和来源校验都按北京时间，快捷区间也从北京时间的今天往前算。
function daysBefore(days: number) {
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai' }).format(new Date())
  return toQuery(new Date(Date.parse(`${today}T00:00:00Z`) - days * 86400000).toISOString().slice(0, 10))
}

function Notes({ notes }: { notes?: string[] }) {
  return <>{notes?.map(note => <p key={note} className="data-note">{note}</p>)}</>
}

function Inputs({ inputs }: { inputs?: FxObservation[] | null }) {
  if (!inputs?.length) return null
  return <div className="table-scroll"><table className="data-table"><thead><tr><th>输入</th><th>数值</th><th>观测时间</th><th>说明</th></tr></thead>
    <tbody>{inputs.map(item => <tr key={item.pair}><td>{item.pair}</td><td>{rate(item.value)}</td><td>{timeText(item.observed_at ?? item.date)}</td><td>{item.note ?? ''}</td></tr>)}</tbody></table></div>
}

// 报价只有最新一笔：逐行显示，同时保留观测时间和未计算的原因。
function SnapshotTable({ data }: { data: FxDataset }) {
  const rows = data.observations ?? []
  const derived = rows.some(item => item.is_derived)
  return <section className="page-panel" aria-label="最新报价">
    <div className="section-caption"><span>{data.provider} · {priceTypes[data.price_type] ?? data.price_type}</span><span>时区 {data.timezone}</span></div>
    {rows.length ? <div className="table-scroll"><table className="data-table"><thead><tr><th>货币对</th><th>数值</th>{derived ? <th>公式</th> : <><th>买入</th><th>卖出</th><th>开盘</th><th>最高</th><th>最低</th></>}<th>观测时间</th><th>说明</th></tr></thead>
      <tbody>{rows.map(item => <tr key={item.pair}><td>{item.pair}</td><td>{rate(item.value)}</td>{derived ? <td>{item.formula}</td> : <><td>{rate(item.bid)}</td><td>{rate(item.ask)}</td><td>{rate(item.open)}</td><td>{rate(item.high)}</td><td>{rate(item.low)}</td></>}<td>{timeText(item.observed_at ?? item.alignment?.time_end ?? item.date)}</td><td>{item.note ?? ''}</td></tr>)}</tbody></table></div>
      : <div className="empty-state">暂无报价</div>}
    {rows.flatMap(item => item.inputs?.length ? [<div key={item.pair} className="fx-inputs"><div className="section-caption"><span>{item.pair} 的输入 · {alignmentLabels[item.alignment?.status ?? ''] ?? item.alignment?.status}</span></div><Inputs inputs={item.inputs} /></div>] : [])}
    <Notes notes={data.notes} />
  </section>
}

function DailySeries({ data, pair, onPair }: { data: FxDataset; pair: string; onPair: (pair: string) => void }) {
  const [page, setPage] = useState(1)
  const observations = useMemo(() => data.observations ?? [], [data])
  const pairs = useMemo(() => [...new Set(observations.map(item => item.pair))].sort(), [observations])
  const selected = pairs.includes(pair) ? pair : pairs[0]
  const rows = useMemo(() => observations.filter(item => item.pair === selected).sort((a, b) => a.date.localeCompare(b.date)), [observations, selected])
  const hasBar = rows.some(item => item.open != null)
  const derived = rows.some(item => item.is_derived)
  const pageSize = 30, shown = [...rows].reverse().slice((page - 1) * pageSize, page * pageSize)
  const source = rows[rows.length - 1]?.source
  if (!rows.length) return <section className="page-panel"><div className="empty-state">所选区间没有数据，请调整日期。</div><Notes notes={data.notes} /></section>
  return <section className="page-panel" aria-label="汇率走势与明细">
    <div className="section-toolbar"><h2>{selected}</h2><span className="data-note">{data.begin} 至 {data.end} · {rows.length} 条 · {data.provider} · {priceTypes[data.price_type] ?? data.price_type}{data.observation_time ? ` · ${data.observation_time}（${data.timezone}）` : ''}</span></div>
    {pairs.length > 1 && <div className="segmented" role="group" aria-label="货币对">{pairs.map(item => <button key={item} aria-pressed={item === selected} onClick={() => { onPair(item); setPage(1) }}>{item}</button>)}</div>}
    {derived && rows[0].formula && <p className="data-note">公式：{rows[0].formula}</p>}
    <BondChart key={selected} title={`${selected} · ${priceTypes[data.price_type] ?? data.price_type}`} points={rows.map(item => ({ label: item.date, value: item.value }))} format={rate} tick={value => value.toFixed(4)} minPadding={0} emptyText="此区间暂无可用数据" valueLabel="单位为每 1 单位基础货币兑换的计价货币" axisLabel="日期" />
    <div className="table-scroll"><table className="data-table"><thead><tr><th>日期</th><th>数值</th>{hasBar && <><th>开盘</th><th>最高</th><th>最低</th></>}{derived && <th>输入</th>}<th>说明</th></tr></thead>
      <tbody>{shown.map(item => <tr key={item.date}><td>{item.date}</td><td>{rate(item.value)}</td>{hasBar && <><td>{rate(item.open)}</td><td>{rate(item.high)}</td><td>{rate(item.low)}</td></>}{derived && <td>{item.inputs?.map(input => `${input.pair} ${rate(input.value)}`).join(' · ')}</td>}<td>{item.note ?? ''}</td></tr>)}</tbody></table></div>
    <Pagination page={page} total={rows.length} hasMore={page * pageSize < rows.length} loading={false} onPage={setPage} />
    {source && <p className="data-note">来源：{source}</p>}
    <Notes notes={data.notes} />
  </section>
}

function SpreadCard({ data }: { data: FxSpread }) {
  return <section className="page-panel" aria-label="在岸离岸价差">
    <div className="section-caption"><span>{data.formula}（推导值）</span><span>{alignmentLabels[data.alignment.status] ?? data.alignment.status}</span></div>
    {data.difference_rmb_per_usd === null && <div className="error" role="alert">未计算：{data.alignment.note ?? data.alignment.status}</div>}
    <dl className="quote-grid">
      <div><dt>绝对价差（元/美元）</dt><dd>{number(data.difference_rmb_per_usd, 6)}</dd></div>
      <div><dt>点差（1点=0.0001）</dt><dd>{number(data.pips, 2)}</dd></div>
      <div><dt>相对价差（%）</dt><dd>{number(data.relative_percent, 4)}</dd></div>
      <div><dt>相对价差（bp）</dt><dd>{number(data.relative_bps, 2)}</dd></div>
      <div><dt>隐含 CNH/CNY</dt><dd>{number(data.implied_cnh_cny, 6)}</dd></div>
    </dl>
    <Inputs inputs={data.inputs} />
    <p className="data-note">计算时间：{timeText(data.as_of)}{data.alignment.skew_seconds != null ? ` · 输入时间差 ${data.alignment.skew_seconds} 秒` : ''}</p>
    <Notes notes={data.notes} />
  </section>
}

interface Query { begin: string; end: string; source: string; pair: string; at: string; maxSkew: string; maxAge: string; refresh: boolean }
const emptyQuery: Query = { begin: '', end: '', source: '', pair: '', at: '', maxSkew: '', maxAge: '', refresh: false }

function FxView({ view, crossPairs }: { view: View; crossPairs: Record<string, string[]> }) {
  const [query, setQuery] = useState<Query>({ ...emptyQuery, source: view === 'history' ? 'sina' : view === 'cross' ? 'fred' : '', pair: view === 'cross' ? 'HKD/CNY' : '' })
  const [draft, setDraft] = useState({ begin: '', end: '' })
  const [validation, setValidation] = useState('')
  // 查询期间数据会被清空，选中的货币对放在这一层。
  const [displayPair, setDisplayPair] = useState('')
  const datedSource = (source: string) => view !== 'quote' && view !== 'spread' && !(view === 'cross' && source === 'sina')
  const hasDates = datedSource(query.source)
  const usesQuotePolicy = view === 'spread' || (view === 'cross' && query.source === 'sina')
  const usesAt = view === 'reference' || (view === 'cross' && query.source === 'cfets')
  const load = useCallback(async (signal: AbortSignal) => {
    const params: Record<string, string> = { command: view }
    if (query.source) params.source = query.source
    if (query.pair) params.pair = query.pair
    if (query.begin) params.begin = query.begin
    if (query.end) params.end = query.end
    if (query.at) params.at = query.at
    if (query.maxSkew) params.max_skew = query.maxSkew
    if (query.maxAge) params.max_age = query.maxAge
    if (query.refresh) params.refresh = 'true'
    const data = await fxData(params, signal)
    const begin = 'begin' in data ? data.begin : undefined, end = 'end' in data ? data.end : undefined
    // 只回填仍为空的输入框，不覆盖用户正在输入的日期。
    if (begin && end) setDraft(current => ({ begin: current.begin || toInput(begin), end: current.end || toInput(end) }))
    return data
  }, [view, query])
  const resource = useResource(load)
  // 提交当前输入的日期和选项；日期不合法时不发请求。
  const apply = (changes: Partial<Query>) => {
    // 按切换后的来源判断：新浪交叉汇率不用日期，输入框里的日期留到切回时再用。
    const dated = datedSource(changes.source ?? query.source)
    const begin = dated ? toQuery(draft.begin) : '', end = dated ? toQuery(draft.end) : ''
    const problem = dated && [begin, end].some(value => value && !/^\d{8}$/.test(value)) ? '日期格式为 2026-01-31' : begin && end && begin > end ? '开始日期不能晚于结束日期' : ''
    if (problem && hasDates) { setValidation(problem); return }
    setValidation('')
    // 从不显示日期的视图切回时用户无法修改日期，无效日期改用默认范围。
    if (problem) {
      setDraft({ begin: '', end: '' })
      setQuery({ ...query, begin: '', end: '', refresh: false, ...changes })
      return
    }
    setQuery({ ...query, begin, end, refresh: false, ...changes })
  }
  const changeSource = (source: string) => {
    const pairs = crossPairs[source] ?? []
    apply({ source, pair: view === 'cross' && !pairs.includes(query.pair) ? 'HKD/CNY' : query.pair, at: '', maxSkew: '', maxAge: '' })
  }
  const quick = (days: number) => {
    const begin = daysBefore(days)
    setValidation('')
    setDraft({ begin: toInput(begin), end: '' })
    setQuery({ ...query, begin, end: '', refresh: false })
  }
  const submit = (event: FormEvent) => { event.preventDefault(); apply({}) }
  const data = resource.data
  return <div className="bond-page">
    <section className="page-panel" aria-label="汇率查询">
      <form className="section-toolbar bond-range-form" onSubmit={submit}>
        {(view === 'history' || view === 'cross') && <label>来源<select aria-label="来源" value={query.source} onChange={event => changeSource(event.target.value)}>{(view === 'history' ? ['sina', 'fred'] : ['fred', 'cfets', 'sina']).map(item => <option key={item} value={item}>{sourceLabels[item]}</option>)}</select></label>}
        {view === 'cross' && <label>货币对<select aria-label="货币对" value={query.pair} onChange={event => apply({ pair: event.target.value })}>{(crossPairs[query.source] ?? []).map(item => <option key={item} value={item}>{item}</option>)}</select></label>}
        {usesAt && <label>时点<select aria-label="时点" value={query.at || '16:00'} onChange={event => apply({ at: event.target.value })}>{atTimes.map(item => <option key={item} value={item}>{item}</option>)}</select></label>}
        {usesQuotePolicy && <label>报价年龄上限<select aria-label="报价年龄上限" value={query.maxAge || '15m'} onChange={event => apply({ maxAge: event.target.value })}><option value="15m">15 分钟</option><option value="1h">1 小时</option><option value="24h">24 小时</option></select></label>}
        {usesQuotePolicy && <label>时间差上限<select aria-label="时间差上限" value={query.maxSkew || '0s'} onChange={event => apply({ maxSkew: event.target.value })}><option value="0s">0 秒（严格）</option><option value="60s">60 秒（近似）</option></select></label>}
        {hasDates && <>
          <label>开始<input aria-label="开始" type="date" value={draft.begin} onChange={event => setDraft({ ...draft, begin: event.target.value })} /></label>
          <label>结束<input aria-label="结束" type="date" value={draft.end} onChange={event => setDraft({ ...draft, end: event.target.value })} /></label>
          <button className="primary-button" type="submit">查询</button>
        </>}
        <PageActions><button type="button" disabled={resource.loading} onClick={() => { setValidation(''); setQuery({ ...query, refresh: true }) }}>刷新</button></PageActions>
      </form>
      {hasDates && <div className="bond-quick-ranges"><span className="data-note">快捷区间</span>{quickRanges.map(([days, label]) => <button key={days} type="button" onClick={() => quick(days)}>{label}</button>)}</div>}
      <p className="data-note">{views.find(item => item.id === view)?.hint}</p>
      {validation && <div className="error" role="alert">{validation}</div>}
    </section>
    <ResourceState {...resource} retry={resource.reload} />
    {data && view === 'spread' && <SpreadCard data={data as FxSpread} />}
    {data && view !== 'spread' && (hasDates ? <DailySeries data={data as FxDataset} pair={displayPair} onPair={setDisplayPair} /> : <SnapshotTable data={data as FxDataset} />)}
  </div>
}

export default function Fx() {
  const [view, setView] = useState<View>('quote')
  const options = useResource(fxOptions)
  return <>
    <div className="segmented fx-views" role="group" aria-label="汇率栏目">{views.map(item => <button key={item.id} aria-pressed={view === item.id} onClick={() => setView(item.id)}>{item.title}</button>)}</div>
    <ResourceState {...options} retry={options.reload} />
    {options.data && <FxView key={view} view={view} crossPairs={options.data.cross_pairs} />}
  </>
}
