import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'

interface Point { label: string; value: number | null }

// 收益率只有单日值，以折线展示；缺失期限或日期保留断点，不插值为 0%。
export default function BondChart({ points, title, curve = false }: { points: Point[]; title: string; curve?: boolean }) {
  const container = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(480)
  const [selected, setSelected] = useState<number | null>(null)
  useEffect(() => {
    if (!container.current) return
    const observer = new ResizeObserver(entries => setWidth(Math.max(200, entries[0].contentRect.width)))
    observer.observe(container.current)
    return () => observer.disconnect()
  }, [])
  const values = points.flatMap(point => point.value !== null && Number.isFinite(point.value) ? [point.value] : [])
  if (!points.length || !values.length) return <div className="empty-state">此区间暂无可用收益率数据</div>
  const left = 58, right = width - 14, top = 16, bottom = 196, height = 250
  const min = Math.min(...values), max = Math.max(...values), padding = Math.max((max - min) * .15, .05)
  const low = min - padding, high = max + padding
  const x = (i: number) => points.length === 1 ? (left + right) / 2 : left + i * (right - left) / (points.length - 1)
  const y = (value: number) => bottom - (value - low) / (high - low) * (bottom - top)
  const current = Math.max(0, Math.min(selected ?? points.length - 1, points.length - 1))
  const point = points[current]
  const tickCount = curve ? Math.max(2, Math.min(points.length, Math.floor((right - left) / 48))) : Math.min(3, points.length)
  const ticks = [...new Set(Array.from({ length: tickCount }, (_, i) => Math.round(i * (points.length - 1) / Math.max(1, tickCount - 1))))]
  let continued = false
  const path = points.map((point, i) => {
    if (point.value === null || !Number.isFinite(point.value)) { continued = false; return '' }
    const command = continued ? 'L' : 'M'
    continued = true
    return `${command}${x(i)},${y(point.value)}`
  }).join(' ')
  const selectPointer = (event: PointerEvent<SVGSVGElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    const position = (event.clientX - bounds.left) * width / bounds.width
    setSelected(Math.max(0, Math.min(points.length - 1, Math.round((position - left) / (right - left) * (points.length - 1)))))
  }
  const selectKey = (event: KeyboardEvent<SVGSVGElement>) => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    setSelected(event.key === 'Home' ? 0 : event.key === 'End' ? points.length - 1 : Math.max(0, Math.min(points.length - 1, current + (event.key === 'ArrowLeft' ? -1 : 1))))
  }
  return <div className="history-chart bond-chart" ref={container}>
    <div className="section-caption"><span>{title}</span><span>{point.label} · {point.value === null ? '—' : `${point.value.toFixed(2)}%`}</span></div>
    <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} role="img" tabIndex={0} aria-label={`${title}，收益率单位百分比；左右方向键选择${curve ? '期限' : '日期'}`} onPointerMove={selectPointer} onPointerDown={selectPointer} onPointerLeave={() => setSelected(null)} onKeyDown={selectKey}>
      {[0, .5, 1].map(t => { const value = low + (high - low) * t; return <g key={t}><line className="chart-grid" x1={left} x2={right} y1={y(value)} y2={y(value)} /><text x={left - 8} y={y(value) + 4} textAnchor="end">{value.toFixed(2)}%</text></g> })}
      <path className="bond-line" d={path} />
      {points.length < 40 && points.map((point, i) => point.value === null ? null : <circle key={i} className="bond-point" cx={x(i)} cy={y(point.value)} r="2.5" />)}
      {selected !== null && <line className="chart-grid" x1={x(current)} x2={x(current)} y1={top} y2={bottom} strokeDasharray="3 4" />}
      {point.value !== null && <circle className="bond-point" cx={x(current)} cy={y(point.value)} r="4" />}
      {ticks.map((i, index) => <text key={i} x={x(i)} y={218} textAnchor={index === 0 ? 'start' : index === ticks.length - 1 ? 'end' : 'middle'}>{curve ? points[i].label : points[i].label.slice(2)}</text>)}
      <text x={(left + right) / 2} y={244} textAnchor="middle">{curve ? '期限（等距排列）' : '交易日期'}</text>
    </svg>
  </div>
}
