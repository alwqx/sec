import type { SecurityQuote } from '../types'
import { priceDigits } from '../hooks/useQuotes'
import { tradeAmount, tradeShares } from '../format'

interface Props {
  quote: SecurityQuote | null
}

function changePct(q: SecurityQuote): number {
  if (!q.yclose) return 0
  return ((q.current - q.yclose) / q.yclose) * 100
}

export default function QuoteCard({ quote: q }: Props) {
  if (!q) {
    return <div className="quote empty">选择一只证券查看行情</div>
  }

  // 没有昨收（新基金没有上一日净值）时不算涨跌。
  const known = q.yclose > 0 && q.current > 0
  const change = known ? q.current - q.yclose : 0
  const pct = changePct(q)
  // A 股习惯：红涨绿跌
  const dir = change > 0 ? 'up' : change < 0 ? 'down' : 'flat'
  const digits = priceDigits(q)
  const fixed = (value: number) => value > 0 ? value.toFixed(digits) : '—'

  return (
    <div className="quote">
      <div className="quote-head">
        <h2>{q.name}</h2>
        <span className="quote-code">{q.ex_code}</span>
      </div>
      <div className={`quote-price ${dir}`}>{q.current.toFixed(digits)}</div>
      <div className={`quote-change ${dir}`}>
        {known ? <>{change > 0 ? '+' : ''}{change.toFixed(digits)}（{pct > 0 ? '+' : ''}{pct.toFixed(2)}%）</> : '—'}
      </div>
      {/* 场外基金只有每天的单位净值，没有开高低和成交。 */}
      {q.nav ? <dl className="quote-grid">
        <div><dt>单位净值</dt><dd>{q.current.toFixed(digits)}</dd></div>
        <div><dt>上一日净值</dt><dd>{fixed(q.yclose)}</dd></div>
        <div><dt>净值日期</dt><dd>{q.trade_date}</dd></div>
      </dl> : <dl className="quote-grid">
        <div>
          <dt>今开</dt>
          <dd>{fixed(q.open)}</dd>
        </div>
        <div>
          <dt>昨收</dt>
          <dd>{fixed(q.yclose)}</dd>
        </div>
        <div>
          <dt>最高</dt>
          <dd>{fixed(q.high)}</dd>
        </div>
        <div>
          <dt>最低</dt>
          <dd>{fixed(q.low)}</dd>
        </div>
        <div>
          <dt>成交额</dt>
          <dd>{tradeAmount(q.volume)}</dd>
        </div>
        <div>
          <dt>成交量</dt>
          <dd>{tradeShares(q.turnover)}</dd>
        </div>
        <div>
          <dt>时间</dt>
          <dd>{q.time}{q.timezone === 'America/New_York' ? ' 美东' : ''}</dd>
        </div>
        <div>
          <dt>日期</dt>
          <dd>{q.trade_date}</dd>
        </div>
      </dl>}
    </div>
  )
}
