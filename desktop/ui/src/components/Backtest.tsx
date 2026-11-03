import { useEffect, useState, type FormEvent, type KeyboardEvent, type PointerEvent } from 'react'
import type { BacktestPerformance, BacktestResult } from '../types'

const signed = (value: number | null) => value === null ? '—' : `${value > 0 ? '+' : ''}${value.toFixed(2)}%`
const drawdown = (p: BacktestPerformance) => p.max_drawdown === 0 ? '0.00%' : `-${p.max_drawdown.toFixed(2)}%`
const money = (value: number) => value.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const lines = [{ key: 'equity' as const, label: '策略', color: 'var(--series-1)' }, { key: 'benchmark' as const, label: '买入持有', color: 'var(--series-2)' }]

// 回测参数以 % 为单位，留空用默认值。
export const backtestFields = [['cash', '初始资金', '100000'], ['commission', '佣金 %', '0.025'], ['min_commission', '最低佣金', '5'], ['stamp_tax', '印花税 %', '0.05'], ['slippage', '滑点 %', '0.1']] as const

function EquityChart({ curve, cash }: { curve: BacktestResult['curve']; cash: number }) {
  const [node, setNode] = useState<HTMLDivElement | null>(null)
  const [width, setWidth] = useState(600)
  const [selected, setSelected] = useState<number | null>(null)
  useEffect(() => {
    if (!node) return
    const observer = new ResizeObserver(entries => setWidth(Math.max(260, entries[0].contentRect.width)))
    observer.observe(node)
    return () => observer.disconnect()
  }, [node])
  const left = 70, right = width - 12, top = 10, bottom = 200, height = 228
  const values = curve.flatMap(point => [point.equity, point.benchmark]).concat(cash)
  const low = Math.min(...values), high = Math.max(...values), pad = (high - low) * .06 || high * .01
  const y = (value: number) => bottom - (value - (low - pad)) / ((high + pad) - (low - pad)) * (bottom - top)
  const x = (index: number) => curve.length === 1 ? (left + right) / 2 : left + index * (right - left) / (curve.length - 1)
  const current = Math.max(0, Math.min(selected ?? curve.length - 1, curve.length - 1))
  const point = curve[current]
  const pick = (event: PointerEvent<SVGSVGElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    setSelected(Math.max(0, Math.min(curve.length - 1, Math.round(((event.clientX - bounds.left) * width / bounds.width - left) / (right - left) * (curve.length - 1)))))
  }
  const key = (event: KeyboardEvent<SVGSVGElement>) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
    event.preventDefault()
    setSelected(Math.max(0, Math.min(curve.length - 1, current + (event.key === 'ArrowLeft' ? -1 : 1))))
  }
  // 持仓的交易日在底部画一条细线，空仓留白。
  const holding = curve.flatMap((p, i) => p.holding ? [i] : [])
  return <div className="chart-box" ref={setNode}>
    <div className="period-readout"><strong>{point.date}</strong>{lines.map(line => <span key={line.key}><b style={{ background: line.color }} />{line.label} {money(point[line.key])}（{signed((point[line.key] / cash - 1) * 100)}）</span>)}<span>{point.holding ? '持仓' : '空仓'}</span></div>
    <svg width={width} height={height} role="img" tabIndex={0} aria-label="回测资产曲线，策略和买入持有，左右方向键选择交易日" onPointerMove={pick} onPointerDown={pick} onPointerLeave={() => setSelected(null)} onKeyDown={key}>
      {[0, .5, 1].map(t => { const value = (low - pad) + ((high + pad) - (low - pad)) * t; return <g key={t}><line className="chart-grid" x1={left} x2={right} y1={y(value)} y2={y(value)} /><text x={left - 6} y={y(value) + 4} textAnchor="end">{Math.round(value).toLocaleString('zh-CN')}</text></g> })}
      <line className="chart-axis" x1={left} x2={right} y1={y(cash)} y2={y(cash)} strokeDasharray="4 4" />
      {lines.map(line => <path key={line.key} className="period-line" stroke={line.color} d={curve.map((p, i) => `${i ? 'L' : 'M'}${x(i)},${y(p[line.key])}`).join(' ')} />)}
      {holding.map(i => <rect key={i} x={x(i) - (right - left) / curve.length / 2} y={bottom + 4} width={Math.max((right - left) / curve.length, 1)} height={3} className="backtest-holding" />)}
      {selected !== null && <line className="chart-grid" x1={x(current)} x2={x(current)} y1={top} y2={bottom} />}
      <text x={left} y={height - 4}>{curve[0].date}</text><text x={right} y={height - 4} textAnchor="end">{curve[curve.length - 1].date}</text>
    </svg>
    <div className="chart-legend">{lines.map(line => <span key={line.key}><b style={{ background: line.color }} />{line.label}</span>)}<span><i className="backtest-holding" />持仓区间</span><span>虚线为初始资金</span></div>
  </div>
}

export default function Backtest({ result, note, draft, setDraft, apply }: { result: BacktestResult | null; note: string; draft: Record<string, string>; setDraft: (draft: Record<string, string>) => void; apply: () => void }) {
  const submit = (event: FormEvent) => { event.preventDefault(); apply() }
  return <section className="backtest" aria-label="回测">
    <div className="section-caption"><h3>回测</h3>{result && <span>{result.begin} 至 {result.end}</span>}</div>
    <form className="section-toolbar strategy-form" onSubmit={submit}>
      {backtestFields.map(([name, label, placeholder]) => <label key={name}>{label}<input aria-label={label} inputMode="decimal" placeholder={placeholder} value={draft[name] ?? ''} onChange={event => setDraft({ ...draft, [name]: event.target.value })} /></label>)}
      <button type="submit">应用</button>
    </form>
    {!result ? <div className="empty-state">{note || '没有回测结果。'}</div> : <>
      <div className="table-scroll"><table className="data-table backtest-summary"><thead><tr><th></th><th>策略</th><th>买入持有</th></tr></thead>
        <tbody>
          <tr><th>总收益</th><td>{signed(result.strategy.total_return)}</td><td>{signed(result.benchmark.total_return)}</td></tr>
          <tr><th>年化收益</th><td>{signed(result.strategy.annualized)}</td><td>{signed(result.benchmark.annualized)}</td></tr>
          <tr><th>最大回撤</th><td>{drawdown(result.strategy)}{result.strategy.max_drawdown > 0 && <small>{result.strategy.drawdown_start} 至 {result.strategy.drawdown_end}</small>}</td><td>{drawdown(result.benchmark)}{result.benchmark.max_drawdown > 0 && <small>{result.benchmark.drawdown_start} 至 {result.benchmark.drawdown_end}</small>}</td></tr>
          <tr><th>期末资产</th><td>{money(result.strategy.final_equity)}</td><td>{money(result.benchmark.final_equity)}</td></tr>
        </tbody></table></div>
      <p className="data-note">完成 {result.round_trips} 轮买卖，胜率 {result.win_rate === null ? '—' : `${result.win_rate.toFixed(0)}%`}，总费用 {money(result.total_fees)} 元，持仓时间 {result.exposure.toFixed(0)}%。{result.open_position && '区间结束时仍持仓，期末资产按最后收盘价计算，没有扣卖出成本。'}</p>
      <EquityChart curve={result.curve} cash={result.config.cash} />
      {result.trades.length > 0 && <div className="table-scroll strategy-signals"><table className="data-table"><thead><tr><th>成交日</th><th>方向</th><th>价格</th><th>股数</th><th>费用</th><th>盈亏</th><th>信号</th></tr></thead>
        <tbody>{[...result.trades].reverse().map(t => <tr key={`${t.date}-${t.side}`}><td>{t.date}</td><td className={t.side === 'buy' ? 'up' : 'down'}>{t.side === 'buy' ? '买入' : '卖出'}</td><td>{t.price.toFixed(3)}</td><td>{t.shares}</td><td>{t.fee.toFixed(2)}</td><td>{t.profit === null ? '—' : `${t.profit > 0 ? '+' : ''}${money(t.profit)}`}</td><td>{t.signal_date} {t.reason}</td></tr>)}</tbody></table></div>}
      {result.events.length > 0 && <p className="data-note">未成交或顺延：{result.events.map(e => `${e.date} ${e.reason}`).join('；')}</p>}
    </>}
    <p className="data-note">信号在收盘产生，下一交易日开盘成交，每次全仓买入或全部卖出；按 100 股一手，开盘涨停买不进、跌停卖不出、停牌顺延，涨跌停幅度按板块（主板 10%，创业板和科创板 20%），ST 股按普通股票处理。使用所选的复权价格，分红没有单独计入现金。结果由规则机械模拟，不构成投资建议。</p>
  </section>
}
