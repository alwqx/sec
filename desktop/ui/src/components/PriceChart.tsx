import { useEffect, useRef, useState } from 'react'

export default function PriceChart({ data, title, unit, label }: {
  data: { date: string; close: number }[]
  title: string
  unit: string
  label: string
}) {
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
  const current = Math.max(0, Math.min(hover ?? data.length - 1, data.length - 1))
  return <div className="history-chart" ref={container}>
    <div className="section-caption"><span>{title}</span><span>{data[current].date} · {data[current].close.toFixed(2)} {unit}</span></div>
    <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`${label}，${data.length} 个交易日，价格单位${unit}`} onPointerLeave={() => setHover(null)} onPointerMove={e => {
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
