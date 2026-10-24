import { useCallback, useState } from 'react'
import { financials } from '../api'
import { useResource } from '../hooks/useResource'
import type { BasicSecurity } from '../types'
import ResourceState from './ResourceState'

function amount(value: unknown, perShare: boolean) {
  if (value === null || value === undefined || value === '') return '—'
  const n = Number(value)
  if (!Number.isFinite(n)) return '—'
  if (perShare) return n.toFixed(2)
  return (n / 1e8).toLocaleString('zh-CN', { maximumFractionDigits: 2 })
}

export default function Financials({ security }: { security: BasicSecurity }) {
  const [report, setReport] = useState('balance')
  const [period, setPeriod] = useState('')
  const load = useCallback((signal: AbortSignal) => financials(security.ex_code, report, period, signal), [security.ex_code, report, period])
  const resource = useResource(load)
  return <section className="research-section">
    <div className="section-toolbar">
      <label>报表<select value={report} onChange={e => setReport(e.target.value)}><option value="balance">资产负债表</option><option value="income">利润表</option><option value="cashflow">现金流量表</option></select></label>
      <label>报告期<select value={period} onChange={e => setPeriod(e.target.value)}><option value="">全部报告期</option><option value="annual">年报</option><option value="halfyear">中报</option><option value="q1">一季报</option><option value="q3">三季报</option></select></label>
    </div>
    <p className="data-note">金额单位：亿元；每股收益单位：元/股。展示数据源最近最多 50 期主要科目。</p>
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && (resource.data.results.length === 0 ? <div className="empty-state">暂无财报数据</div> : <div className="table-scroll"><table className="data-table financial-table">
      <thead><tr><th>科目</th>{resource.data.results.map((item, i) => <th key={`${item.date}-${i}`}>{item.date.slice(0, 10)}</th>)}</tr></thead>
      <tbody>{resource.data.columns.map(col => <tr key={col.key}><th>{col.label}</th>{resource.data!.results.map((item, i) => <td key={`${item.date}-${i}`}>{amount(item.fields[col.key], col.key === 'BASIC_EPS')}</td>)}</tr>)}</tbody>
    </table></div>)}
  </section>
}
