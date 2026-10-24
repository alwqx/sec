import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'
import type { HistoryQuote } from '../types'

function compactNumber(value: number | undefined) {
  if (value === undefined || !Number.isFinite(value)) return '—'
  if (Math.abs(value) >= 1e8) return `${(value / 1e8).toFixed(2)} 亿`
  if (Math.abs(value) >= 1e4) return `${(value / 1e4).toFixed(2)} 万`
  return value.toLocaleString('zh-CN', { maximumFractionDigits: 2 })
}

export default function CandlestickChart({ data, name, unit, showVolume = false }: {
  data: HistoryQuote[]
  name: string
  unit: string
  showVolume?: boolean
}) {
  const container = useRef<HTMLDivElement>(null)
  const scroll = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(480)
  const [selected, setSelected] = useState<number | null>(null)
  useEffect(() => {
    if (!container.current) return
    const observer = new ResizeObserver(entries => setWidth(Math.max(200, entries[0].contentRect.width)))
    observer.observe(container.current)
    return () => observer.disconnect()
  }, [])

  // 每根蜡烛至少预留 8px，长区间通过横向滚动查看，避免压缩成无法分辨的细线。
  // 价格轴固定在滚动区域之外；蜡烛图坐标只包含右侧绘图区。
  const axisWidth = 60, left = 8, right = 16, top = 20, bottom = 234
  const volumeTop = 270, volumeBottom = 338
  const dateY = showVolume ? 362 : 258
  const height = showVolume ? 374 : 270
  const chartWidth = Math.max(width - axisWidth, left + right + data.length * 8)
  useEffect(() => {
    if (scroll.current) scroll.current.scrollLeft = scroll.current.scrollWidth
  }, [chartWidth, data])

  if (!data.length) return <p className="data-note">暂无历史行情</p>
  const minimum = Math.min(...data.map(item => item.low))
  const maximum = Math.max(...data.map(item => item.high))
  const padding = Math.max((maximum - minimum) * .08, maximum * .002, .01)
  const low = minimum - padding, high = maximum + padding
  const step = (chartWidth - left - right) / data.length
  const bodyWidth = Math.min(14, Math.max(3, step * .65))
  const x = (index: number) => left + (index + .5) * step
  const y = (price: number) => bottom - (price - low) / (high - low) * (bottom - top)
  const current = Math.max(0, Math.min(selected ?? data.length - 1, data.length - 1))
  const item = data[current]
  const volume = (candle: HistoryQuote) => candle.volume !== undefined && Number.isFinite(candle.volume) ? Math.max(0, candle.volume) : 0
  const maximumVolume = Math.max(0, ...data.map(volume))
  const volumeY = (value: number) => volumeBottom - value / Math.max(1, maximumVolume) * (volumeBottom - volumeTop)
  const selectPointer = (event: PointerEvent<SVGSVGElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    const position = (event.clientX - bounds.left) * chartWidth / bounds.width
    setSelected(Math.max(0, Math.min(data.length - 1, Math.floor((position - left) / step))))
  }
  const selectKey = (event: KeyboardEvent<SVGSVGElement>) => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? data.length - 1 : current + (event.key === 'ArrowLeft' ? -1 : 1)
    const index = Math.max(0, Math.min(data.length - 1, next))
    setSelected(index)
    if (scroll.current) {
      const viewport = scroll.current
      if (x(index) < viewport.scrollLeft + left) viewport.scrollLeft = x(index) - left
      if (x(index) > viewport.scrollLeft + viewport.clientWidth - right) viewport.scrollLeft = x(index) - viewport.clientWidth + right
    }
  }
  const ticks = [...new Set([0, Math.floor((data.length - 1) / 2), data.length - 1])]

  return <div className="history-chart candle-chart" ref={container}>
    <div className="section-caption"><span>{name} · 日 K 线 · {unit}</span><span>红：收盘高于开盘 · 绿：收盘低于开盘</span></div>
    <div className="candle-detail"><time>{item.date}</time><span>开 {item.open.toFixed(2)}</span><span>高 {item.high.toFixed(2)}</span><span>低 {item.low.toFixed(2)}</span><span>收 {item.close.toFixed(2)}</span>
      {showVolume && <><span>成交量 {compactNumber(item.volume)} 股/份</span><span>成交额 {compactNumber(item.turnover)} {unit}</span></>}
    </div>
    <div className="candle-plot">
      <svg className="candle-axis" width={axisWidth} height={height} viewBox={`0 0 ${axisWidth} ${height}`} aria-hidden="true">
        {[0, .5, 1].map(value => {
          const price = low + (high - low) * value
          return <text key={value} x={axisWidth - 8} y={y(price) + 4} textAnchor="end">{price.toFixed(2)}</text>
        })}
        {showVolume && <>
          <text x={axisWidth - 8} y={volumeTop - 10} textAnchor="end">成交量</text>
          <text x={axisWidth - 8} y={volumeTop + 8} textAnchor="end">{compactNumber(maximumVolume)}</text>
          <text x={axisWidth - 8} y={volumeBottom} textAnchor="end">0</text>
        </>}
      </svg>
      <div className="candle-scroll" ref={scroll}>
        <svg width={chartWidth} height={height} viewBox={`0 0 ${chartWidth} ${height}`} role="img" tabIndex={0}
          aria-label={`${name} 日K线，${data.length} 个交易日，单位${unit}；左右方向键选择交易日，Home 和 End 跳至首尾`}
          onKeyDown={selectKey} onPointerMove={selectPointer} onPointerDown={selectPointer} onPointerLeave={() => setSelected(null)}>
          {[0, .5, 1].map(value => {
            const price = low + (high - low) * value
            return <line key={value} className="chart-grid" x1={left} x2={chartWidth - right} y1={y(price)} y2={y(price)} />
          })}
          {data.map((candle, index) => {
            const direction = candle.close > candle.open ? 'up' : candle.close < candle.open ? 'down' : 'flat'
            const bodyTop = y(Math.max(candle.open, candle.close))
            const bodyHeight = Math.abs(y(candle.open) - y(candle.close))
            return <g key={`${candle.date}-${index}`} className={`candle ${direction}`}>
              <title>{`${candle.date} 开 ${candle.open.toFixed(2)} 高 ${candle.high.toFixed(2)} 低 ${candle.low.toFixed(2)} 收 ${candle.close.toFixed(2)} ${unit}`}</title>
              <line className="candle-wick" x1={x(index)} x2={x(index)} y1={y(candle.high)} y2={y(candle.low)} />
              {bodyHeight < 1 ? <line className="candle-doji" x1={x(index) - bodyWidth / 2} x2={x(index) + bodyWidth / 2} y1={bodyTop} y2={bodyTop} />
                : <rect className="candle-body" x={x(index) - bodyWidth / 2} y={bodyTop} width={bodyWidth} height={bodyHeight} />}
              {showVolume && <rect className="candle-volume" x={x(index) - bodyWidth / 2} y={volumeY(volume(candle))} width={bodyWidth} height={volumeBottom - volumeY(volume(candle))} />}
            </g>
          })}
          {showVolume && <line className="chart-grid" x1={left} x2={chartWidth - right} y1={volumeBottom} y2={volumeBottom} />}
          {selected !== null && <line className="chart-grid" x1={x(current)} x2={x(current)} y1={top} y2={showVolume ? volumeBottom : bottom} strokeDasharray="3 4" />}
          {ticks.map((index, i) => <text key={index} x={i === 0 ? left : i === ticks.length - 1 ? chartWidth - right : x(index)} y={dateY} textAnchor={i === 0 ? 'start' : i === ticks.length - 1 ? 'end' : 'middle'}>{data[index].date}</text>)}
        </svg>
      </div>
    </div>
    <p className="data-note">悬停或点击查看当日开高低收；图表聚焦后可用左右方向键选择。长区间可横向滚动。</p>
  </div>
}
