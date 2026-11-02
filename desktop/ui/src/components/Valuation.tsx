import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent, type PointerEvent } from 'react'
import { valuation } from '../api'
import { useResource } from '../hooks/useResource'
import type { ValuationHistory, ValuationMultiple, ValuationResult } from '../types'
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
  return <>
    <dl className="quote-grid metrics-grid">
      <div><dt>收盘价</dt><dd>{data.price.toFixed(2)}</dd></div>
      <div><dt>总市值</dt><dd>{money(data.market_cap)}</dd></div>
      <div><dt>PE（TTM）</dt><dd>{currentMultiple(data.pe)}</dd></div>
      <div><dt>PB（MRQ）</dt><dd>{currentMultiple(data.pb)}</dd></div>
      <div><dt>PS（TTM）</dt><dd>{currentMultiple(data.ps)}</dd></div>
      <div><dt>PEG</dt><dd>{fixed(data.peg)}</dd></div>
      <div><dt>ROE（{data.roe_year || '—'} 年报加权）</dt><dd>{percent(data.roe)}</dd></div>
      <div><dt>净利润 CAGR{data.growth.rate !== null && `（${data.growth.from_year}–${data.growth.to_year}）`}</dt><dd>{percent(data.growth.rate)}</dd></div>
      <div><dt>每股收益（TTM）</dt><dd>{fixed(data.eps)}</dd></div>
      <div><dt>每股净资产</dt><dd>{fixed(data.bvps)}</dd></div>
      <div><dt>格雷厄姆数</dt><dd>{fixed(data.graham)}</dd></div>
      <div><dt>格雷厄姆数相对收盘价</dt><dd>{data.graham_margin === null ? '—' : signed(data.graham_margin)}</dd></div>
    </dl>
    <p className="data-note">{data.industry || '行业未知'} · 数据日期 {data.trade_date} · PE、PB、PS 由东方财富按收盘价计算，每股收益和每股净资产由这些倍数反推，与年报披露的基本每股收益口径不同。{data.growth.rate === null && data.growth.note && ` 增长率不可用：${data.growth.note}。`}</p>
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
      <tbody>{data.annuals.map(row => <tr key={row.year}><td>{row.year}</td><td>{fixed(row.eps)}</td><td>{fixed(row.bps)}</td><td>{percent(row.roe)}</td><td>{money(row.revenue)}</td><td>{money(row.profit)}</td></tr>)}</tbody></table></div></section>
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
      <div><dt>当前现金流（净利润 × 70%）</dt><dd>{money(dcf.base_cash)}</dd></div>
      <div><dt>增长期现值</dt><dd>{money(dcf.growth_pv)}</dd></div>
      <div><dt>终值现值（占 {dcf.terminal_share?.toFixed(0) ?? '—'}%）</dt><dd>{money(dcf.terminal_pv)}</dd></div>
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
      <section aria-label="规则汇总"><div className="section-caption"><h3>规则汇总：{data.assessment}</h3></div>
        {data.signals.length > 0 && <div className="table-scroll"><table className="data-table"><thead><tr><th>指标</th><th>判断</th><th>依据</th></tr></thead>
          <tbody>{data.signals.map(row => <tr key={row.name}><td>{row.name}</td><td>{levels[row.level]}</td><td>{row.rule}</td></tr>)}</tbody></table></div>}
        <p className="data-note">按上述规则的偏低、偏高个数比较得出，少于两条规则时显示数据不足。这只是机械汇总，未考虑行业差异和公司基本面。</p></section>
      <section aria-label="DCF"><div className="section-caption"><h3>DCF 折现</h3></div>
        <form className="section-toolbar strategy-form" onSubmit={submit}>
          {dcfFields.map(([name, label, placeholder]) => <label key={name}>{label}<input aria-label={label} inputMode="decimal" placeholder={placeholder} value={draft[name]} onChange={event => setDraft({ ...draft, [name]: event.target.value })} /></label>)}
          <button type="submit">应用</button><button type="button" onClick={reset}>恢复默认</button>
        </form>
        {data.dcf.warning && <p className="error" role="note">{data.dcf.warning}</p>}
        <DCFResult data={data} loading={resource.loading} error={resource.error} retry={resource.reload} />
        <p className="data-note">两阶段股权现金流折现：5 年增长期加永续期。现金流用近四个季度归母净利润的 70% 近似，折现结果视为股权价值，未单独扣除净负债。增长率留空时取近 5 年净利润 CAGR，限制在 ±15%；填 0 表示零增长。结果对参数高度敏感。</p></section>
      <Annuals data={data} />
      <p className="data-note">{data.disclaimer} 命令行：sec valuation。</p>
    </>}
  </div>
}
