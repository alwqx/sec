import { useCallback, useEffect, useRef, useState } from 'react'
import { history } from '../api'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'

function Chart({ data, code }: { data: { date: string; close: number }[]; code: string }) {
  const container = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(480)
  const [hover, setHover] = useState<number | null>(null)
  useEffect(() => {
    if (!container.current) return
    const observer = new ResizeObserver(entries => setWidth(Math.max(200, entries[0].contentRect.width)))
    observer.observe(container.current)
    return () => observer.disconnect()
  }, [])
  const height = 210, left = 54, right = width - 12, top = 16, bottom = 176
  const values = data.map(d => d.close)
  const min = Math.min(...values), max = Math.max(...values), padding = Math.max((max - min) * .12, max * .005, .01)
  const low = min - padding, high = max + padding
  const x = (i: number) => data.length === 1 ? (left + right) / 2 : left + i / (data.length - 1) * (right - left)
  const y = (value: number) => bottom - (value - low) / (high - low) * (bottom - top)
  const current = hover === null ? data.length - 1 : hover
  return <div className="history-chart" ref={container}>
    <div className="section-caption"><span>近 60 个交易日 · 收盘价 · 不复权</span><span>{data[current].date} · {data[current].close.toFixed(2)} {code.startsWith('HK') ? '港元' : '元'}</span></div>
    <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`${code} 最近 ${data.length} 个交易日的收盘价走势，价格单位${code.startsWith('HK') ? '港元' : '元'}`} onPointerLeave={() => setHover(null)} onPointerMove={e => {
      const bounds = e.currentTarget.getBoundingClientRect()
      setHover(Math.max(0, Math.min(data.length - 1, Math.round((e.clientX - bounds.left - left) / (right - left) * (data.length - 1)))))
    }}>
      {[0, .5, 1].map(t => { const value = low + (high - low) * t; return <g key={t}><line x1={left} x2={right} y1={y(value)} y2={y(value)} className="chart-grid" /><text x={left - 8} y={y(value) + 4} textAnchor="end">{value.toFixed(2)}</text></g> })}
      <path className="price-line" d={data.map((d, i) => `${i ? 'L' : 'M'}${x(i)},${y(d.close)}`).join(' ')} />
      <circle cx={x(current)} cy={y(data[current].close)} r="3.5" className="price-point" />
      {hover !== null && <line className="chart-grid" x1={x(current)} x2={x(current)} y1={top} y2={bottom} strokeDasharray="3 4" />}
      <text x={left} y={200}>{data[0].date.slice(5)}</text><text x={right} y={200} textAnchor="end">{data[data.length - 1].date.slice(5)}</text>
    </svg>
  </div>
}

function LoadedHistory({ code }: { code: string }) {
  const load = useCallback((signal: AbortSignal) => history(code, signal), [code])
  const resource = useResource(load)
  const values = resource.data?.filter(item => Number.isFinite(item.close)) ?? []
  return <div className="history-section"><ResourceState {...resource} retry={resource.reload} />
    {resource.data && (values.length ? <Chart data={values} code={code} /> : <p className="data-note">暂无历史行情</p>)}
  </div>
}

export default function PriceHistory({ code }: { code: string }) {
  return /^(SH|SZ)\d{6}$|^HK\d{5}$/i.test(code) ? <LoadedHistory code={code} /> : <p className="data-note">走势图暂支持沪深证券与港股。</p>
}
