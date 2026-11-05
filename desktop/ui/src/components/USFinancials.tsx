import { useCallback, useState } from 'react'
import { usFinancials } from '../api'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'
import type { USReport } from '../types'

const statements = { income: '利润表', balance: '资产负债表', cashflow: '现金流量表' } as const
type StatementKey = keyof typeof statements

// unitLabel 返回行名后的单位：金额是亿加列报币种，每股金额是币种，股数是亿股。
function unitLabel(unit: string, report: USReport): string {
  if (unit === 'shares') return '亿股'
  if (unit.endsWith('/shares')) return report.currency_name
  return unit === report.currency ? `亿${report.currency_name}` : ''
}

// USFinancials 是美股的财报栏：SEC XBRL 数据整理成三张报表的主要项目，报告期为列，从新到旧。
export default function USFinancials({ code }: { code: string }) {
  const [statement, setStatement] = useState<StatementKey>('income')
  const [quarterly, setQuarterly] = useState(false)
  const load = useCallback((signal: AbortSignal) => usFinancials(code, statement, quarterly ? 'quarter' : 'annual', signal), [code, statement, quarterly])
  const resource = useResource(load)
  const report = resource.data?.report
  const value = (raw: number, unit: string) => unit.endsWith('/shares') ? raw.toFixed(2) : (raw / 1e8).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
  const lines = report?.lines.filter(line => report.periods.some(period => period.values[line.key] !== undefined && period.values[line.key] !== null)) ?? []
  const derived = report?.periods.some(period => period.derived?.length)
  return <section className="research-section">
    <div className="section-toolbar">
      <div className="segmented" role="group" aria-label="报表">{(Object.keys(statements) as StatementKey[]).map(item => <button key={item} type="button" aria-pressed={statement === item} onClick={() => setStatement(item)}>{statements[item]}</button>)}</div>
      <div className="segmented" role="group" aria-label="报告期">{[[false, '年度'], [true, '季度']].map(([item, label]) => <button key={String(item)} type="button" aria-pressed={quarterly === item} onClick={() => setQuarterly(item as boolean)}>{label}</button>)}</div>
    </div>
    {resource.data && <p className="data-note">{resource.data.name}（{resource.data.ticker}，CIK {resource.data.cik}）· SEC EDGAR XBRL 财务数据（{report?.standard ?? 'US GAAP'}）{report && report.currency !== 'USD' && `，金额以${report.currency_name}列报`}</p>}
    <ResourceState {...resource} retry={resource.reload} />
    {report && (report.periods.length === 0 ? <div className="empty-state">{!report.quarterly ? '没有这张报表的数据' : report.standard === 'IFRS' ? '没有季度数据；按 IFRS 申报的外国公司在 SEC 只提交 20-F 年报，请查看年度报表。' : '没有季度数据；部分外国公司只披露年报，不提交 10-Q 季报。'}</div> : <div className="table-scroll"><table className="data-table us-financials-table">
      <thead><tr><th>项目</th>{report.periods.map(period => <th key={period.end}>FY{period.fiscal_year}{report.quarterly ? period.fiscal_period : ''}<small>{period.end}</small></th>)}</tr></thead>
      <tbody>{lines.map(line => <tr key={line.key}><th scope="row">{line.label}<small>{unitLabel(line.unit, report)}</small></th>{report.periods.map(period => {
        const raw = period.values[line.key]
        const mark = period.derived?.includes(line.key)
        return <td key={period.end} className={raw !== undefined && raw !== null && raw < 0 ? 'down' : undefined} title={mark ? '推算值' : undefined}>{raw === undefined || raw === null ? '—' : value(raw, line.unit) + (mark ? '*' : '')}</td>
      })}</tr>)}</tbody>
    </table></div>)}
    <p className="data-note">同一报告期有重述时用最新申报的数值；财年按截止日所在年份标注。{derived && '* 推算值：没有单季数时用年初累计数相减（第四季度 = 全年 - 前三季度），自由现金流 = 经营活动现金流 - 资本支出，没有申报归母股东权益时 = 股东权益合计 - 少数股东权益。'}</p>
  </section>
}
