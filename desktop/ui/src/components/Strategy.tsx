import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent, type PointerEvent } from 'react'
import { strategy } from '../api'
import { useResource } from '../hooks/useResource'
import type { StrategyResult } from '../types'
import ResourceState from './ResourceState'
import { frequencies, marketToday, monthsAgo, rangeLabel, type Frequency } from '../marketDate'

const kinds = {
  ma: { title: '双均线', params: [['fast', '快线', '5'], ['slow', '慢线', '20']], rule: '快线上穿慢线为买入，下穿为卖出。' },
  macd: { title: 'MACD', params: [['fast', '快线', '12'], ['slow', '慢线', '26'], ['signal', '信号线', '9']], rule: 'MACD 线上穿信号线为买入，下穿为卖出。' },
  rsi: { title: 'RSI', params: [['period', '周期', '14'], ['oversold', '超卖', '30'], ['overbought', '超买', '70']], rule: 'RSI 从超卖区回升为买入，从超买区回落为卖出。' },
  boll: { title: '布林带', params: [['period', '周期', '20'], ['k', '倍数', '2']], rule: '收盘价从下轨外回升为买入，从上轨外回落为卖出。' },
} as const
type Kind = keyof typeof kinds
const adjustments = { qfq: '前复权', bfq: '不复权', hfq: '后复权' }
const lineClasses = ['strategy-line-0', 'strategy-line-1', 'strategy-line-2']

function StrategyChart({ data, name }: { data: StrategyResult; name: string }) {
  const container = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(600)
  const [selected, setSelected] = useState<number | null>(null)
  useEffect(() => {
    if (!container.current) return
    const observer = new ResizeObserver(entries => setWidth(Math.max(240, entries[0].contentRect.width)))
    observer.observe(container.current)
    return () => observer.disconnect()
  }, [])
  const count = data.dates.length
  const priceSeries = data.series.filter(item => item.panel === 'price')
  const indicatorSeries = data.series.filter(item => item.panel === 'indicator')
  const hasIndicator = indicatorSeries.length > 0
  const left = 58, right = width - 14, priceTop = 12, priceBottom = 220, indicatorTop = 250, indicatorBottom = 350
  const axisY = (hasIndicator ? indicatorBottom : priceBottom) + 18
  const height = axisY + 8
  const x = (index: number) => count === 1 ? (left + right) / 2 : left + index * (right - left) / (count - 1)
  const known = (values: (number | null)[]) => values.flatMap(value => value === null ? [] : [value])
  const scale = (values: number[], top: number, bottom: number) => {
    const low = Math.min(...values), high = Math.max(...values)
    const padding = (high - low) * .08 || Math.abs(high) * .01 || 1
    return { low: low - padding, high: high + padding, y: (value: number) => bottom - (value - (low - padding)) / (high + padding - (low - padding)) * (bottom - top) }
  }
  const price = scale([...data.closes, ...priceSeries.flatMap(item => known(item.values))], priceTop, priceBottom)
  const indicatorValues = [...indicatorSeries.flatMap(item => known(item.values)), ...(data.levels ?? [])]
  // RSI 的取值范围是 0 到 100，固定坐标轴，阈值线的位置也不会随数据变化。
  const fixedRange = (low: number, high: number) => ({ low, high, y: (value: number) => indicatorBottom - (value - low) / (high - low) * (indicatorBottom - indicatorTop) })
  const indicator = !hasIndicator || !indicatorValues.length ? null : data.levels?.length ? fixedRange(0, 100)
    : scale([...indicatorValues, ...(indicatorSeries.some(item => item.style === 'bar') ? [0] : [])], indicatorTop, indicatorBottom)
  const path = (values: (number | null)[], y: (value: number) => number) => {
    let continued = false
    return values.map((value, index) => {
      if (value === null) { continued = false; return '' }
      const command = continued ? 'L' : 'M'
      continued = true
      return `${command}${x(index)},${y(value)}`
    }).join(' ')
  }
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
  const dateIndex = new Map(data.dates.map((date, index) => [date, index]))
  const barWidth = Math.max(1, Math.min(8, (right - left) / count * .6))
  return <div className="history-chart" ref={container}>
    <div className="candle-detail"><time>{data.dates[current]}</time><span>收盘 {data.closes[current].toFixed(2)}</span>
      {data.series.map(item => <span key={item.label}>{item.label} {item.values[current]?.toFixed(2) ?? '—'}</span>)}</div>
    <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} role="img" tabIndex={0} aria-label={`${name} 技术分析，${count} 根 K 线；左右方向键选择交易日`} onKeyDown={selectKey} onPointerMove={selectPointer} onPointerDown={selectPointer} onPointerLeave={() => setSelected(null)}>
      {[0, .5, 1].map(t => { const value = price.low + (price.high - price.low) * t; return <g key={t}><line className="chart-grid" x1={left} x2={right} y1={price.y(value)} y2={price.y(value)} /><text x={left - 8} y={price.y(value) + 4} textAnchor="end">{value.toFixed(2)}</text></g> })}
      <path className="strategy-close" d={path(data.closes, price.y)} />
      {priceSeries.map((item, index) => <path key={item.label} className={`strategy-line ${lineClasses[index % 3]}`} d={path(item.values, price.y)} />)}
      {data.signals.map(signal => { const index = dateIndex.get(signal.date); if (index === undefined) return null; const px = x(index), py = price.y(signal.price); return signal.type === 'buy'
        ? <path key={signal.date} className="strategy-buy" d={`M${px},${py + 6} l-5,8 h10 z`}><title>{signal.date} 买入信号：{signal.reason}</title></path>
        : <path key={signal.date} className="strategy-sell" d={`M${px},${py - 6} l-5,-8 h10 z`}><title>{signal.date} 卖出信号：{signal.reason}</title></path> })}
      {indicator && <>
        {[0, .5, 1].map(t => { const value = indicator.low + (indicator.high - indicator.low) * t; return <g key={t}><line className="chart-grid" x1={left} x2={right} y1={indicator.y(value)} y2={indicator.y(value)} /><text x={left - 8} y={indicator.y(value) + 4} textAnchor="end">{value.toFixed(2)}</text></g> })}
        {data.levels?.map(level => <line key={level} className="strategy-level" x1={left} x2={right} y1={indicator.y(level)} y2={indicator.y(level)} />)}
        {indicatorSeries.filter(item => item.style === 'bar').flatMap(item => item.values.map((value, index) => value === null ? null : <rect key={`${item.label}-${index}`} className={value >= 0 ? 'strategy-bar-up' : 'strategy-bar-down'} x={x(index) - barWidth / 2} width={barWidth} y={Math.min(indicator.y(value), indicator.y(0))} height={Math.abs(indicator.y(value) - indicator.y(0))} />))}
        {indicatorSeries.filter(item => item.style === 'line').map((item, index) => <path key={item.label} className={`strategy-line ${lineClasses[index % 3]}`} d={path(item.values, indicator.y)} />)}
      </>}
      {selected !== null && <line className="chart-grid" x1={x(current)} x2={x(current)} y1={priceTop} y2={hasIndicator ? indicatorBottom : priceBottom} strokeDasharray="3 4" />}
      <text x={left} y={axisY}>{data.dates[0]}</text><text x={right} y={axisY} textAnchor="end">{data.dates[count - 1]}</text>
    </svg>
    <div className="strategy-legend"><span className="strategy-swatch-close">收盘</span>{data.series.filter(item => item.style === 'line').map(item => { const group = data.series.filter(other => other.panel === item.panel && other.style === 'line'); return <span key={item.label} className={`strategy-swatch ${lineClasses[group.indexOf(item) % 3]}`}>{item.label}</span> })}<span className="up">▲ 买入信号</span><span className="down">▼ 卖出信号</span></div>
  </div>
}

function defaultParams(kind: Kind) {
  return Object.fromEntries(kinds[kind].params.map(([name, , value]) => [name, value])) as Record<string, string>
}

function LoadedStrategy({ code }: { code: string }) {
  const [kind, setKind] = useState<Kind>('ma')
  const [fq, setFq] = useState<keyof typeof adjustments>('qfq')
  const [freq, setFreq] = useState<Frequency>('day')
  const [range, setRange] = useState(() => ({ begin: monthsAgo(12), end: marketToday() }))
  const [draft, setDraft] = useState(() => defaultParams('ma'))
  const [params, setParams] = useState(draft)
  const load = useCallback((signal: AbortSignal) => strategy({ code, kind, fq, freq, ...range, ...params }, signal), [code, kind, fq, freq, range, params])
  const resource = useResource(load)
  const data = resource.data
  const signals = useMemo(() => [...(data?.signals ?? [])].reverse(), [data])
  const changeKind = (next: Kind) => {
    const values = defaultParams(next)
    setKind(next)
    setDraft(values)
    setParams(values)
  }
  // 日线默认近 1 年；周线和月线的 K 线少，看得更长。
  const changeFreq = (next: Frequency) => {
    if (next === freq) return
    setFreq(next)
    setRange({ begin: monthsAgo(next === 'day' ? 12 : frequencies[next].months), end: marketToday() })
  }
  const submit = (event: FormEvent) => { event.preventDefault(); setParams({ ...draft }) }
  const buys = signals.filter(item => item.type === 'buy').length
  return <div className="strategy-page">
    <div className="section-toolbar"><div className="segmented" role="group" aria-label="策略">{(Object.keys(kinds) as Kind[]).map(item => <button key={item} aria-pressed={kind === item} onClick={() => changeKind(item)}>{kinds[item].title}</button>)}</div>
      <div className="segmented" role="group" aria-label="K 线周期">{(Object.keys(frequencies) as Frequency[]).map(item => <button key={item} aria-pressed={freq === item} onClick={() => changeFreq(item)}>{frequencies[item].title}</button>)}</div>
      <label>复权<select aria-label="复权方式" value={fq} onChange={event => setFq(event.target.value as keyof typeof adjustments)}>{Object.entries(adjustments).map(([value, title]) => <option key={value} value={value}>{title}</option>)}</select></label></div>
    <form className="section-toolbar strategy-form" onSubmit={submit}>
      {kinds[kind].params.map(([name, label]) => <label key={name}>{label}<input aria-label={label} inputMode="decimal" value={draft[name] ?? ''} onChange={event => setDraft({ ...draft, [name]: event.target.value })} /></label>)}
      <button type="submit">应用</button>
      <div className="history-shortcuts">{frequencies[freq].ranges.map(months => <button key={months} type="button" onClick={() => setRange({ begin: monthsAgo(months), end: marketToday() })}>{rangeLabel(months)}</button>)}</div>
    </form>
    <p className="data-note">{range.begin} 至 {range.end} · {frequencies[freq].title} · {adjustments[fq]} · {kinds[kind].rule}</p>
    <ResourceState {...resource} retry={resource.reload} />
    {data?.source_note && <p className="data-note">{data.source_note}</p>}
    {data && (data.insufficient ? <div className="empty-state">数据不足：至少需要 {data.required} 根 K 线。</div> : data.dates.length === 0 ? <div className="empty-state">所选区间没有交易记录。</div> : <>
      <StrategyChart key={`${kind}-${freq}-${data.dates.length}`} data={data} name={code} />
      <div className="section-caption"><span>信号 {signals.length} 个（买入 {buys} · 卖出 {signals.length - buys}）</span></div>
      {signals.length ? <div className="table-scroll strategy-signals"><table className="data-table"><thead><tr><th>日期</th><th>信号</th><th>收盘价</th><th>原因</th></tr></thead>
        <tbody>{signals.map(item => <tr key={item.date}><td>{item.date}</td><td className={item.type === 'buy' ? 'up' : 'down'}>{item.type === 'buy' ? '买入' : '卖出'}</td><td>{item.price.toFixed(2)}</td><td>{item.reason}</td></tr>)}</tbody></table></div> : <div className="empty-state">所选区间没有触发信号。</div>}
    </>)}
    <p className="data-note">信号由规则机械生成，未计入交易成本和滑点，不是回测结果，也不构成投资建议。默认前复权，避免除权缺口产生假信号；命令行 sec strategy 使用不复权价格，结果可能不同。</p>
  </div>
}

export default function Strategy({ code }: { code: string }) {
  return /^(SH|SZ)\d{6}$|^HK\d{5}$/i.test(code) ? <LoadedStrategy code={code} /> : <div className="empty-state">技术分析暂支持沪深证券与港股。</div>
}
