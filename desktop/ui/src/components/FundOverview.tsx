import { useCallback } from 'react'
import { fundOverview } from '../api'
import { money } from '../format'
import { useResource } from '../hooks/useResource'
import type { FundReturns } from '../types'
import ResourceState from './ResourceState'

const fixed = (value: number | null, digits: number, suffix = '') => value === null ? '—' : `${value.toFixed(digits)}${suffix}`
const signed = (value: number | null) => value === null ? '—' : `${value > 0 ? '+' : ''}${value.toFixed(2)}%`
const direction = (value: number | null) => value === null || value === 0 ? '' : value > 0 ? 'up' : 'down'
const periods: [keyof FundReturns, string][] = [['1w', '近1周'], ['1m', '近1月'], ['3m', '近3月'], ['6m', '近6月'], ['1y', '近1年'], ['2y', '近2年'], ['3y', '近3年'], ['ytd', '今年来'], ['since_launch', '成立来']]

// FundOverview 是基金的概览栏：净值或货币基金收益、阶段收益率和基金档案。
export default function FundOverview({ code }: { code: string }) {
  const load = useCallback((signal: AbortSignal) => fundOverview(code, signal), [code])
  const resource = useResource(load)
  const data = resource.data
  const cards: [string, string, string?][] = !data ? [] : data.money_market ? [
    ['万份收益', fixed(data.yield_10k, 4, ' 元')], ['七日年化', fixed(data.yield_7d, 3, '%')],
  ] : [
    ['单位净值', fixed(data.nav, 4)], ['累计净值', fixed(data.acc_nav, 4)], ['日涨幅', signed(data.day_change), direction(data.day_change)],
  ]
  const rows: [string, string][] = data ? [
    ['基金全称', data.full_name], ['基金类型', data.type], ['场内交易', data.listed ? '是' : '否'], ['风险等级', data.risk_level],
    ['基金管理人', data.company], ['基金托管人', data.custodian], ['基金经理', data.managers],
    ['成立日期', [data.launch_date, data.launch_size].filter(Boolean).join(' / ')],
    ['管理费率', data.management_fee], ['托管费率', data.custody_fee], ['业绩比较基准', data.benchmark], ['跟踪标的', data.tracking],
  ] : []
  return <section className="research-section">
    <ResourceState {...resource} retry={resource.reload} />
    {data && <>
      {data.notes.map(note => <p key={note} className="data-note">{note}</p>)}
      <h2>{data.name}</h2>
      <dl className="quote-grid">
        {cards.map(([label, value, dir]) => <div key={label}><dt>{label}</dt><dd className={dir}>{value}</dd></div>)}
        <div><dt>净值日期</dt><dd>{data.nav_date || '—'}</dd></div>
        {/* 净资产只显示一个值：App 接口的较新，没有时用网页带截止日期的季报值。 */}
        <div><dt>净资产</dt><dd>{data.net_assets !== null ? money(data.net_assets) : data.net_assets_note || '—'}</dd></div>
      </dl>
      {data.returns && <div className="table-scroll"><table className="data-table"><thead><tr>{periods.map(([, label]) => <th key={label}>{label}</th>)}</tr></thead>
        <tbody><tr>{periods.map(([key, label]) => <td key={label} className={direction(data.returns![key])}>{signed(data.returns![key])}</td>)}</tr></tbody></table></div>}
      <div className="table-scroll"><table className="data-table us-company-table hk-company-table"><tbody>
        {rows.map(([label, value]) => <tr key={label}><th scope="row">{label}</th><td>{value || '—'}</td></tr>)}
      </tbody></table></div>
      {data.objective && <p className="company-description">投资目标：{data.objective}</p>}
      <p className="data-note">净值、收益率和风险等级来自天天基金 App 接口，基金档案来自天天基金网页；红涨绿跌。</p>
    </>}
  </section>
}
