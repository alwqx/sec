import { useCallback, useState } from 'react'
import { usFinancials } from '../api'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'

const statements = { income: '利润表', balance: '资产负债表', cashflow: '现金流量表' } as const
type StatementKey = keyof typeof statements
const units: Record<string, string> = { USD: '亿美元', 'USD/shares': '美元', shares: '亿股' }

// USFinancials 是美股的财报栏：SEC XBRL 数据整理成三张报表的主要项目，报告期为列，从新到旧。
export default function USFinancials({ code }: { code: string }) {
  const [statement, setStatement] = useState<StatementKey>('income')
  const [quarterly, setQuarterly] = useState(false)
  const load = useCallback((signal: AbortSignal) => usFinancials(code, statement, quarterly ? 'quarter' : 'annual', signal), [code, statement, quarterly])
  const resource = useResource(load)
  const report = resource.data?.report
  const value = (raw: number, unit: string) => unit === 'USD/shares' ? raw.toFixed(2) : (raw / 1e8).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
  const lines = report?.lines.filter(line => report.periods.some(period => period.values[line.key] !== undefined && period.values[line.key] !== null)) ?? []
  const derived = report?.periods.some(period => period.derived?.length)
  return <section className="research-section">
    <div className="section-toolbar">
      <div className="segmented" role="group" aria-label="报表">{(Object.keys(statements) as StatementKey[]).map(item => <button key={item} type="button" aria-pressed={statement === item} onClick={() => setStatement(item)}>{statements[item]}</button>)}</div>
      <div className="segmented" role="group" aria-label="报告期">{[[false, '年度'], [true, '季度']].map(([item, label]) => <button key={String(item)} type="button" aria-pressed={quarterly === item} onClick={() => setQuarterly(item as boolean)}>{label}</button>)}</div>
    </div>
    {resource.data && <p className="data-note">{resource.data.name}（{resource.data.ticker}，CIK {resource.data.cik}）· SEC EDGAR XBRL 财务数据（US GAAP）</p>}
    <ResourceState {...resource} retry={resource.reload} />
    {report && (report.periods.length === 0 ? <div className="empty-state">{report.quarterly ? '没有季度数据；部分外国公司只披露年报，不提交 10-Q 季报。' : '没有这张报表的数据'}</div> : <div className="table-scroll"><table className="data-table us-financials-table">
      <thead><tr><th>项目</th>{report.periods.map(period => <th key={period.end}>FY{period.fiscal_year}{report.quarterly ? period.fiscal_period : ''}<small>{period.end}</small></th>)}</tr></thead>
      <tbody>{lines.map(line => <tr key={line.key}><th scope="row">{line.label}<small>{units[line.unit]}</small></th>{report.periods.map(period => {
        const raw = period.values[line.key]
        const mark = period.derived?.includes(line.key)
        return <td key={period.end} className={raw !== undefined && raw !== null && raw < 0 ? 'down' : undefined} title={mark ? '推算值' : undefined}>{raw === undefined || raw === null ? '—' : value(raw, line.unit) + (mark ? '*' : '')}</td>
      })}</tr>)}</tbody>
    </table></div>)}
    <p className="data-note">同一报告期有重述时用最新申报的数值；财年按截止日所在年份标注。{derived && '* 推算值：没有单季数时用年初累计数相减（第四季度 = 全年 - 前三季度），自由现金流 = 经营活动现金流 - 资本支出。'}外国公司按国际会计准则（IFRS）申报的暂不支持。</p>
  </section>
}
