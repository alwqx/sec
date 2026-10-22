import type { SecurityQuote } from '../types'

interface Props {
  quote: SecurityQuote | null
}

function changePct(q: SecurityQuote): number {
  if (!q.yclose) return 0
  return ((q.current - q.yclose) / q.yclose) * 100
}

function fmtAmount(v: number): string {
  if (v >= 1e8) return `${(v / 1e8).toFixed(2)} 亿`
  if (v >= 1e4) return `${(v / 1e4).toFixed(2)} 万`
  return `${v.toFixed(0)}`
}

function fmtShares(v: number): string {
  if (v >= 1e8) return `${(v / 1e8).toFixed(2)} 亿股`
  if (v >= 1e4) return `${(v / 1e4).toFixed(2)} 万股`
  return `${v} 股`
}

export default function QuoteCard({ quote: q }: Props) {
  if (!q) {
    return <div className="quote empty">选择一只证券查看行情</div>
  }

  const change = q.current - q.yclose
  const pct = changePct(q)
  // A 股习惯：红涨绿跌
  const dir = change > 0 ? 'up' : change < 0 ? 'down' : 'flat'

  return (
    <div className="quote">
      <div className="quote-head">
        <h2>{q.name}</h2>
        <span className="quote-code">{q.ex_code}</span>
      </div>
      <div className={`quote-price ${dir}`}>{q.current.toFixed(2)}</div>
      <div className={`quote-change ${dir}`}>
        {change > 0 ? '+' : ''}
        {change.toFixed(2)}（{pct > 0 ? '+' : ''}
        {pct.toFixed(2)}%）
      </div>
      <dl className="quote-grid">
        <div>
          <dt>今开</dt>
          <dd>{q.open.toFixed(2)}</dd>
        </div>
        <div>
          <dt>昨收</dt>
          <dd>{q.yclose.toFixed(2)}</dd>
        </div>
        <div>
          <dt>最高</dt>
          <dd>{q.high.toFixed(2)}</dd>
        </div>
        <div>
          <dt>最低</dt>
          <dd>{q.low.toFixed(2)}</dd>
        </div>
        <div>
          <dt>成交额</dt>
          <dd>{fmtAmount(q.volume)}</dd>
        </div>
        <div>
          <dt>成交量</dt>
          <dd>{fmtShares(q.turnover)}</dd>
        </div>
        <div>
          <dt>时间</dt>
          <dd>{q.time}</dd>
        </div>
        <div>
          <dt>日期</dt>
          <dd>{q.trade_date}</dd>
        </div>
      </dl>
    </div>
  )
}
