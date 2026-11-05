import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent, type PointerEvent } from 'react'
import { valuation } from '../api'
import { useResource } from '../hooks/useResource'
import type { ValuationDividends, ValuationHistory, ValuationMultiple, ValuationPeers, ValuationResult } from '../types'
import { money } from '../format'
import ResourceState from './ResourceState'

const multiples = { pe: 'PE（TTM）', pb: 'PB（MRQ）', ps: 'PS（TTM）' } as const
type MultipleKey = keyof typeof multiples
const levels = { low: '偏低', neutral: '中性', high: '偏高' }
// 留空时由后端决定默认值，实际使用的参数显示在结果里。
const dcfFields = [['growth', '增长率 %', '自动'], ['terminal', '永续增长率 %', '默认'], ['wacc', '折现率 %', '默认'], ['margin', '安全边际 %', '默认']] as const
const defaultParams: Record<string, string> = { growth: '', terminal: '', wacc: '', margin: '' }

const fixed = (value: number | null | undefined, digits = 2) => value === null || value === undefined ? '—' : value.toFixed(digits)
const percent = (value: number | null | undefined, digits = 1) => value === null || value === undefined ? '—' : `${value.toFixed(digits)}%`
const signed = (value: number) => `${value > 0 ? '+' : ''}${value.toFixed(1)}%`
// unitOf 是金额单位：美股为美元。
function unitOf(data: ValuationResult) {
  return data.currency === 'USD' ? '美元' : '元'
}

function currentMultiple(item: ValuationMultiple) {
  if (item.current !== null) return item.current.toFixed(2)
  return item.raw !== null ? `不适用（上游值 ${item.raw.toFixed(2)}）` : '—'
}

function HistoryChart({ dates, item, label }: { dates: string[]; item: ValuationMultiple; label: string }) {
  if (!item.history) return <div className="empty-state">{label}没有可用的历史数据（亏损或净资产为负的交易日不计入）。</div>
  return <HistoryLines dates={dates} item={item} label={label} history={item.history} />
}

function HistoryLines({ dates, item, label, history }: { dates: string[]; item: ValuationMultiple; label: string; history: ValuationHistory }) {
  const container = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(600)
  const [selected, setSelected] = useState<number | null>(null)
  useEffect(() => {
    if (!container.current) return
    const observer = new ResizeObserver(entries => setWidth(Math.max(240, entries[0].contentRect.width)))
    observer.observe(container.current)
    return () => observer.disconnect()
  }, [])
  const count = dates.length
  const left = 58, right = width - 14, top = 12, bottom = 200, height = 226
  const padding = (history.max - history.min) * .08 || history.max * .01 || 1
  const low = history.min - padding, high = history.max + padding
  const x = (index: number) => count === 1 ? (left + right) / 2 : left + index * (right - left) / (count - 1)
  const y = (value: number) => bottom - (value - low) / (high - low) * (bottom - top)
  // 移动光标只改变选中日期，路径不用重算。
  const { path, dots } = useMemo(() => {
    let continued = false
    const path = item.series.map((value, index) => {
      if (value === null) { continued = false; return '' }
      const command = continued ? 'L' : 'M'
      continued = true
      return `${command}${x(index)},${y(value)}`
    }).join(' ')
    const dots = item.series.flatMap((value, index) => value !== null && item.series[index - 1] == null && item.series[index + 1] == null ? [{ index, value }] : [])
    return { path, dots }
    // x 和 y 只由下面这些值决定。
  }, [item.series, width, low, high])
  const current = Math.max(0, Math.min(selected ?? count - 1, count - 1))
  const selectPointer = (event: PointerEvent<SVGSVGElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    const position = (event.clientX - bounds.left) * width / bounds.width
    setSelected(Math.max(0, Math.min(count - 1, Math.round((position - left) / (right - left) * (count - 1)))))
  }
  const selectKey = (event: KeyboardEvent<SVGSVGElement>) => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    setSelected(event.key === 'Home' ? 0 : event.key === 'End' ? count - 1 : Math.max(0, Math.min(count - 1, current + (event.key === 'ArrowLeft' ? -1 : 1))))
  }
  return <div className="history-chart" ref={container}>
    <div className="candle-detail"><time>{dates[current]}</time><span>{label} {fixed(item.series[current])}</span><span>中位 {history.median.toFixed(2)}</span></div>
    <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} role="img" tabIndex={0} aria-label={`${label}历史，${count} 个交易日；左右方向键选择交易日`} onKeyDown={selectKey} onPointerMove={selectPointer} onPointerDown={selectPointer} onPointerLeave={() => setSelected(null)}>
      {[history.min, history.max].map((value, index) => <g key={index}><line className="chart-grid" x1={left} x2={right} y1={y(value)} y2={y(value)} /><text x={left - 8} y={y(value) + 4} textAnchor="end">{value.toFixed(2)}</text></g>)}
      <line className="strategy-level" x1={left} x2={right} y1={y(history.median)} y2={y(history.median)} /><text x={left - 8} y={y(history.median) + 4} textAnchor="end">{history.median.toFixed(2)}</text>
      <path className="strategy-line strategy-line-0" d={path} />
      {dots.map(dot => <circle key={dot.index} className="valuation-dot" cx={x(dot.index)} cy={y(dot.value)} r={2} />)}
      {selected !== null && <line className="chart-grid" x1={x(current)} x2={x(current)} y1={top} y2={bottom} strokeDasharray="3 4" />}
      <text x={left} y={bottom + 18}>{dates[0]}</text><text x={right} y={bottom + 18} textAnchor="end">{dates[count - 1]}</text>
    </svg>
  </div>
}

function Summary({ data }: { data: ValuationResult }) {
  const us = data.currency === 'USD'
  return <>
    <dl className="quote-grid metrics-grid">
      <div><dt>收盘价{us && '（美元）'}</dt><dd>{data.price.toFixed(2)}</dd></div>
      <div><dt>总市值</dt><dd>{money(data.market_cap, unitOf(data))}</dd></div>
      <div><dt>PE（TTM）</dt><dd>{currentMultiple(data.pe)}</dd></div>
      <div><dt>PB（MRQ）</dt><dd>{currentMultiple(data.pb)}</dd></div>
      <div><dt>PS（TTM）</dt><dd>{currentMultiple(data.ps)}</dd></div>
      <div><dt>PEG</dt><dd>{fixed(data.peg)}</dd></div>
      <div><dt>ROE（{data.roe_year || '—'} {us ? '财年，期末权益' : '年报加权'}）</dt><dd>{percent(data.roe)}</dd></div>
      <div><dt>净利润 CAGR{data.growth.rate !== null && `（${data.growth.from_year}–${data.growth.to_year}）`}</dt><dd>{percent(data.growth.rate)}</dd></div>
      <div><dt>每股收益（TTM）</dt><dd>{fixed(data.eps)}</dd></div>
      <div><dt>每股净资产</dt><dd>{fixed(data.bvps)}</dd></div>
      <div><dt>格雷厄姆数</dt><dd>{fixed(data.graham)}</dd></div>
      <div><dt>格雷厄姆数相对收盘价</dt><dd>{data.graham_margin === null ? '—' : signed(data.graham_margin)}</dd></div>
    </dl>
    <p className="data-note">{data.industry || '行业未知'} · 数据日期 {data.trade_date} · {us ? '美股估值由 SEC XBRL 财务数据和新浪不复权日线计算：市值 = 收盘价 × 当时申报的流通股数（封面股数；只按类别申报时从申报封面页读取各类股数，按新浪的当前股数折算成所查这类股票；都没有时用季度稀释加权股数；拆股后按比例调整），PE、PS 用最近四个季度，PB 用最近一期股东权益，只用当天之前已经申报的数值，报告期后来被重述时用重述后的数字；美股没有同行业对比。' : 'PE、PB、PS 由东方财富按收盘价计算，'}每股收益和每股净资产由这些倍数反推，与年报披露的{us ? '稀释' : '基本'}每股收益口径不同。{data.growth.rate === null && data.growth.note && ` 增长率不可用：${data.growth.note}。`}</p>
  </>
}

function Multiples({ data }: { data: ValuationResult }) {
  const [key, setKey] = useState<MultipleKey>('pe')
  const item = data[key]
  return <section aria-label="历史估值">
    <div className="section-toolbar"><div className="segmented" role="group" aria-label="估值倍数">{(Object.keys(multiples) as MultipleKey[]).map(name => <button key={name} aria-pressed={key === name} onClick={() => setKey(name)}>{multiples[name]}</button>)}</div>
      {item.history && <span className="data-note">{item.history.since} 以来 {item.history.samples} 个交易日 · {item.history.short ? '样本不足，不计算分位和情景' : `当前分位 ${percent(item.history.percentile, 0)}`}</span>}</div>
    <HistoryChart key={key} dates={data.dates} item={item} label={multiples[key]} />
    {item.scenarios.length > 0 && <div className="table-scroll"><table className="data-table"><thead><tr><th>假设倍数</th><th>对应股价</th><th>相对收盘价</th></tr></thead>
      <tbody>{item.scenarios.map(row => <tr key={row.label}><td>{row.label}（{row.multiple.toFixed(2)}）</td><td>{row.price.toFixed(2)}</td><td>{signed(row.change)}</td></tr>)}</tbody></table></div>}
    <p className="data-note">分位是不高于当前值的交易日占比，亏损或净资产为负的交易日不计入，少于 250 个交易日时不计算。情景只调整倍数，假设盈利、净资产和营收不变。</p>
  </section>
}

function Annuals({ data }: { data: ValuationResult }) {
  if (!data.annuals.length) return null
  return <section aria-label="年报数据"><div className="section-caption"><h3>年报</h3></div>
    <div className="table-scroll valuation-annuals"><table className="data-table"><thead><tr><th>年度</th><th>基本每股收益</th><th>每股净资产</th><th>加权 ROE</th><th>营业总收入</th><th>归母净利润</th></tr></thead>
      <tbody>{data.annuals.map(row => <tr key={row.year}><td>{row.year}</td><td>{fixed(row.eps)}</td><td>{fixed(row.bps)}</td><td>{percent(row.roe)}</td><td>{money(row.revenue, unitOf(data))}</td><td>{money(row.profit, unitOf(data))}</td></tr>)}</tbody></table></div></section>
}

// 默认列出的公司数，按总市值排序。
const peerListSize = 10
const peerMultiple = (value: number | null) => value === null || value <= 0 ? '—' : value.toFixed(2)

function Peers({ peers, note }: { peers: ValuationPeers | null; note: string }) {
  const [all, setAll] = useState(false)
  if (!peers) return <section aria-label="同行业对比"><div className="section-caption"><h3>同行业对比</h3></div><div className="empty-state">{note}</div></section>
  const items = all ? peers.items : peers.items.filter((item, index) => index < peerListSize || item.self)
  return <section aria-label="同行业对比"><div className="section-caption"><h3>同行业对比：{peers.board}</h3><span>{peers.date} · 共 {peers.items.length} 家</span></div>
    <div className="table-scroll"><table className="data-table"><thead><tr><th>指标</th><th>当前值</th><th>行业中位</th><th>相对中位</th><th>排名（低到高）</th><th>分位</th><th>参与比较</th></tr></thead>
      <tbody>{(Object.keys(multiples) as MultipleKey[]).map(key => { const item = peers[key]; return <tr key={key}><td>{multiples[key]}</td><td>{fixed(item.current)}</td><td>{item.short ? '样本不足' : fixed(item.median)}</td><td>{item.premium === null ? '—' : signed(item.premium)}</td><td>{item.rank === null ? '—' : `${item.rank} / ${item.samples}`}</td><td>{percent(item.percentile, 0)}</td><td>{item.samples} 家，排除 {item.excluded} 家</td></tr> })}</tbody></table></div>
    <div className="table-scroll valuation-peers"><table className="data-table"><thead><tr><th>公司</th><th>代码</th><th>总市值</th><th>PE</th><th>PB</th><th>PS</th></tr></thead>
      <tbody>{items.map(item => <tr key={item.code} className={item.self ? 'valuation-peer-self' : undefined}><td>{item.name}</td><td>{item.code}</td><td>{money(item.market_cap)}</td><td>{peerMultiple(item.pe)}</td><td>{peerMultiple(item.pb)}</td><td>{peerMultiple(item.ps)}</td></tr>)}</tbody></table></div>
    {peers.items.length > peerListSize && <button type="button" onClick={() => setAll(!all)}>{all ? `只看总市值前 ${peerListSize} 家` : `显示全部 ${peers.items.length} 家`}</button>}
    <p className="data-note">比较同一交易日、东方财富同一行业的公司。倍数小于等于 0 或缺失的公司不参与比较，中位数偏向盈利公司；少于 5 家时不计算中位数和排名。行业内公司的业务和增长可能差别很大。</p>
  </section>
}

function Dividends({ dividends, note, us }: { dividends: ValuationDividends | null; note: string; us: boolean }) {
  if (!dividends) return <section aria-label="分红"><div className="section-caption"><h3>分红</h3></div><div className="empty-state">{note}</div></section>
  const { history, years, pending, mismatches } = dividends
  return <section aria-label="分红"><div className="section-caption"><h3>分红</h3></div>
    <dl className="quote-grid metrics-grid">
      <div><dt>股息率{dividends.fiscal_year && `（${dividends.fiscal_year} 财年）`}</dt><dd>{percent(dividends.yield, 2)}</dd></div>
      <div><dt>每股分红</dt><dd>{dividends.per_share.toFixed(3)}</dd></div>
      <div><dt>近 5 个财年平均股息率</dt><dd>{percent(dividends.average_5y, 2)}</dd></div>
      <div><dt>历史中位</dt><dd>{percent(history?.median, 2)}</dd></div>
      <div><dt>历史范围</dt><dd>{history ? `${history.min.toFixed(2)}% ~ ${history.max.toFixed(2)}%` : '—'}</dd></div>
      <div><dt>历史分位</dt><dd>{history?.short ? '样本不足' : percent(history?.percentile, 0)}</dd></div>
      <div><dt>连续分红</dt><dd>{dividends.consecutive_years} 个财年</dd></div>
    </dl>
    {years.length > 0 ? <div className="table-scroll valuation-annuals"><table className="data-table"><thead><tr><th>财年</th><th>每股现金分红</th><th>{us ? '稀释每股收益' : '基本每股收益'}</th><th>分红率</th></tr></thead>
      <tbody>{years.map(row => <tr key={row.year}><td>{row.year}</td><td>{row.per_share.toFixed(3)}</td><td>{fixed(row.eps)}</td><td>{percent(row.payout)}</td></tr>)}</tbody></table></div> : <div className="empty-state">有分红记录，但没有实施过现金分红。</div>}
    {!us && !dividends.mismatch_checked && <p className="data-note">未能和新浪核对，东方财富可能缺少特别分红，股息率可能偏低。</p>}
    {mismatches.length > 0 && <p className="error" role="note">近 5 年有 {mismatches.length} 次分红和新浪记录不一致，股息率按东方财富计算，可能有偏差：{mismatches.map(item => `${item.ex_date} 新浪 ${item.sina.toFixed(4)}，东方财富 ${item.east_money === null ? '缺少' : item.east_money.toFixed(4)}`).join('；')}</p>}
    {pending.length > 0 && <p className="data-note">未除息，不计入股息率：{pending.map(item => `${item.report} ${item.progress} ${item.plan}`).join('；')}</p>}
    {us ? <p className="data-note">股息率是最近一个完整财年支付的现金股息总额（现金流量表）除以当前市值，不受拆股影响，上方的每股分红是支付总额除以当前股数；财年表的每股现金分红取年报披露的每股宣告股息，没有时用支付总额除以稀释股数；分红率 = 支付的股息 ÷ 归母净利润。历史分位只比较有分红的交易日，只用当天之前已经申报的年报。分红可能不持续。</p>
    : <p className="data-note">股息率是最近完整财年（年度分红已除息）的税前每股现金分红，按当前股本调整送转后除以收盘价，不含送转、配股和回购。最近一次分红超过 18 个月时按 0 计算。历史分位只比较有分红的交易日。东方财富可能缺少特别分红，近 5 年的记录会尽量和新浪核对，不一致或没能核对时在上方说明。分红可能不持续。财年表按报告期归属，金额和每股收益都是当时的股本口径；没有年度分红或财年内发生送转时不计算分红率。</p>}
  </section>
}

function DCFResult({ data, loading, error, retry }: { data: ValuationResult; loading: boolean; error: string; retry: () => void }) {
  const dcf = data.dcf
  if (loading || error) return <ResourceState loading={loading} error={error} retry={retry} />
  if (!dcf.applicable) return <div className="empty-state">{dcf.note}</div>
  return <>
    <dl className="quote-grid metrics-grid">
      <div><dt>每股价值</dt><dd>{fixed(dcf.per_share)}</dd></div>
      <div><dt>扣除安全边际后</dt><dd>{fixed(dcf.fair_price)}</dd></div>
      <div><dt>相对收盘价</dt><dd>{dcf.upside === null ? '—' : signed(dcf.upside)}</dd></div>
      <div><dt>当前现金流（净利润 × 70%）</dt><dd>{money(dcf.base_cash, unitOf(data))}</dd></div>
      <div><dt>增长期现值</dt><dd>{money(dcf.growth_pv, unitOf(data))}</dd></div>
      <div><dt>终值现值（占 {dcf.terminal_share?.toFixed(0) ?? '—'}%）</dt><dd>{money(dcf.terminal_pv, unitOf(data))}</dd></div>
    </dl>
    <p className="data-note">增长率 {percent(dcf.params.growth)}：{dcf.growth_source}。永续增长率 {percent(dcf.params.terminal_growth)}，折现率 {percent(dcf.params.wacc)}，安全边际 {percent(dcf.params.margin_of_safety)}。</p>
  </>
}

export default function Valuation({ code }: { code: string }) {
  const [draft, setDraft] = useState(defaultParams)
  const [params, setParams] = useState(defaultParams)
  const load = useCallback((signal: AbortSignal) => valuation({ code, ...params }, signal), [code, params])
  const resource = useResource(load)
  // 改 DCF 参数时保留已有内容，参数有误也不能让表单消失。
  const last = useRef<ValuationResult | null>(null)
  if (resource.data) last.current = resource.data
  const data = resource.data ?? last.current
  const submit = (event: FormEvent) => { event.preventDefault(); setParams({ ...draft }) }
  const reset = () => { setDraft(defaultParams); setParams(defaultParams) }
  return <div className="valuation-page">
    {!data && <ResourceState {...resource} retry={resource.reload} />}
    {data && <>
      <Summary data={data} />
      <Multiples data={data} />
      <Peers peers={data.peers} note={data.peers_note} />
      <Dividends dividends={data.dividends} note={data.dividends_note} us={data.currency === 'USD'} />
      <section aria-label="规则汇总"><div className="section-caption"><h3>规则汇总：{data.assessment}</h3></div>
        {data.signals.length > 0 && <div className="table-scroll"><table className="data-table"><thead><tr><th>指标</th><th>判断</th><th>依据</th></tr></thead>
          <tbody>{data.signals.map(row => <tr key={row.name}><td>{row.name}</td><td>{levels[row.level]}</td><td>{row.rule}</td></tr>)}</tbody></table></div>}
        <p className="data-note">按上述规则的偏低、偏高个数比较得出，少于两条规则时显示数据不足；同行业参与比较的公司少于 10 家时不计入行业规则。这只是机械汇总，未考虑公司基本面。{data.peers_note && ` ${data.peers_note}。`}{data.dividends_note && ` ${data.dividends_note}。`}</p></section>
      <section aria-label="DCF"><div className="section-caption"><h3>DCF 折现</h3></div>
        <form className="section-toolbar strategy-form" onSubmit={submit}>
          {dcfFields.map(([name, label, placeholder]) => <label key={name}>{label}<input aria-label={label} inputMode="decimal" placeholder={placeholder} value={draft[name]} onChange={event => setDraft({ ...draft, [name]: event.target.value })} /></label>)}
          <button type="submit">应用</button><button type="button" onClick={reset}>恢复默认</button>
        </form>
        {data.dcf.warning && <p className="error" role="note">{data.dcf.warning}</p>}
        <DCFResult data={data} loading={resource.loading} error={resource.error} retry={resource.reload} />
        <p className="data-note">两阶段股权现金流折现：5 年增长期加永续期。现金流用近四个季度归母净利润的 70% 近似，折现结果视为股权价值，未单独扣除净负债。增长率留空时取近 5 年净利润 CAGR，限制在 ±15%；填 0 表示零增长。结果对参数高度敏感。</p></section>
      <Annuals data={data} />
      {data.shares_note && <p className="data-note">股数说明：{data.shares_note}</p>}
      <p className="data-note">{data.disclaimer} 命令行：sec valuation。</p>
    </>}
  </div>
}
