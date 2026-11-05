import { useCallback } from 'react'
import { usDividends } from '../api'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'
import { yi } from '../format'

const fixed = (value: number | null, digits: number, suffix = '', sign = false) => value === null ? '—' : (sign && value > 0 ? '+' : '') + value.toFixed(digits) + suffix

// USDividends 是美股的分红栏：SEC XBRL 年报整理的按财年分红表，和 CLI 的 sec info -d 相同，从新到旧。
export default function USDividends({ code }: { code: string }) {
  const load = useCallback((signal: AbortSignal) => usDividends(code, signal), [code])
  const resource = useResource(load)
  const data = resource.data
  const name = data?.currency_name ?? '美元'
  const splits = data?.splits.map(s => s.factor >= 1 ? `${s.date} 1 拆 ${s.factor}` : `${s.date} ${1 / s.factor} 合 1`).join('，')
  return <section className="research-section">
    {data && <p className="data-note">{data.name}（{data.ticker}，CIK {data.cik}）· SEC EDGAR XBRL 年报{data.currency !== 'USD' && `，金额以${name}列报`}</p>}
    <ResourceState {...resource} retry={resource.reload} />
    {data && (data.years.length === 0 ? <div className="empty-state">SEC 年报中没有分红和回购记录，可能不分红。</div> : <div className="table-scroll"><table className="data-table">
      <thead><tr><th>财年</th><th>截止日</th><th>每股股息<small>{name}</small></th><th>同比</th><th>支付股息<small>亿{name}</small></th><th>归母净利润<small>亿{name}</small></th><th>分红率</th><th>回购<small>亿{name}</small></th></tr></thead>
      <tbody>{data.years.map(y => <tr key={y.end}>
        <th scope="row">{y.fiscal_year}</th><td>{y.end}</td><td>{fixed(y.per_share, 2)}</td>
        <td className={y.growth !== null && y.growth < 0 ? 'down' : undefined}>{fixed(y.growth, 1, '%', true)}</td>
        <td>{yi(y.paid)}</td><td className={y.net_income !== null && y.net_income < 0 ? 'down' : undefined}>{yi(y.net_income)}</td>
        <td>{fixed(y.payout, 1, '%')}</td><td>{yi(y.buyback)}</td>
      </tr>)}</tbody>
    </table></div>)}
    <p className="data-note">每股股息是年报的宣告数，支付股息和回购来自现金流量表，分红率 = 支付股息 ÷ 归母净利润；SEC 数据没有除权除息日、登记日和派息日。
      {!data?.years.length ? '' : data.splits_unchecked ? '新浪日线没有读到，无法识别拆股，拆股前年报的每股股息没有按拆股调整。' : splits ? `每股股息已按拆股统一到当前股本口径（${splits}）。` : ''}
      股息率见估值栏目。</p>
  </section>
}
