import { useCallback, useEffect, useRef, useState } from 'react'
import { exportFinancials, financials } from '../api'
import { useResource } from '../hooks/useResource'
import type { BasicSecurity, SavedDownload as SavedFile } from '../types'
import ResourceState from './ResourceState'
import AnnualReports from './AnnualReports'
import SavedDownload from './SavedDownload'

function amount(value: unknown, perShare: boolean) {
  if (value === null || value === undefined || value === '') return '—'
  const n = Number(value)
  if (!Number.isFinite(n)) return '—'
  if (perShare) return n.toFixed(2)
  return (n / 1e8).toLocaleString('zh-CN', { maximumFractionDigits: 2 })
}

function FinancialStatements({ security }: { security: BasicSecurity }) {
  const [report, setReport] = useState('balance')
  const [period, setPeriod] = useState('')
  const [exporting, setExporting] = useState('')
  const [saved, setSaved] = useState<SavedFile | null>(null)
  const [exportError, setExportError] = useState('')
  const controller = useRef<AbortController | null>(null)
  useEffect(() => () => controller.current?.abort(), [])
  const save = async (format: 'csv' | 'json') => {
    if (exporting) return
    const request = new AbortController()
    controller.current = request
    setExporting(format)
    setSaved(null)
    setExportError('')
    try {
      const file = await exportFinancials(security.ex_code, report, period, format, request.signal)
      if (!request.signal.aborted) setSaved(file)
    } catch (e) {
      if (!request.signal.aborted) setExportError(e instanceof Error ? e.message : String(e))
    } finally { if (!request.signal.aborted) setExporting('') }
  }
  const load = useCallback((signal: AbortSignal) => financials(security.ex_code, report, period, signal), [security.ex_code, report, period])
  const resource = useResource(load)
  return <section className="research-section">
    <div className="section-toolbar">
      <label>报表<select disabled={!!exporting} value={report} onChange={e => setReport(e.target.value)}><option value="balance">资产负债表</option><option value="income">利润表</option><option value="cashflow">现金流量表</option></select></label>
      <label>报告期<select disabled={!!exporting} value={period} onChange={e => setPeriod(e.target.value)}><option value="">全部报告期</option><option value="annual">年报</option><option value="halfyear">中报</option><option value="q1">一季报</option><option value="q3">三季报</option></select></label>
    </div>
    <div className="financial-export-actions">
      <button disabled={!!exporting || resource.loading || !resource.data?.results.length} onClick={() => void save('csv')}>{exporting === 'csv' ? '正在导出 CSV…' : '导出 CSV'}</button>
      <button disabled={!!exporting || resource.loading || !resource.data?.results.length} onClick={() => void save('json')}>{exporting === 'json' ? '正在导出 JSON…' : '导出 JSON'}</button>
    </div>
    <p className="data-note">按当前报表类型与报告期重新获取并导出全部原始字段，最多 50 期；导出数值保持原始单位，不按亿元缩放。文件保存到设置中的下载目录（默认 ~/Downloads），同名文件自动编号。</p>
    {exportError && <p className="error" role="alert">{exportError}</p>}
    {saved && <SavedDownload file={saved} />}
    <p className="data-note">金额单位：亿元；每股收益单位：元/股。展示数据源最近最多 50 期主要科目。</p>
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && (resource.data.results.length === 0 ? <div className="empty-state">暂无财报数据</div> : <div className="table-scroll"><table className="data-table financial-table">
      <thead><tr><th>科目</th>{resource.data.results.map((item, i) => <th key={`${item.date}-${i}`}>{item.date.slice(0, 10)}</th>)}</tr></thead>
      <tbody>{resource.data.columns.map(col => <tr key={col.key}><th>{col.label}</th>{resource.data!.results.map((item, i) => <td key={`${item.date}-${i}`}>{amount(item.fields[col.key], col.key === 'BASIC_EPS')}</td>)}</tr>)}</tbody>
    </table></div>)}
  </section>
}

export default function Financials({ security }: { security: BasicSecurity }) {
  const [view, setView] = useState<'statements' | 'annual'>('statements')
  return <div className="financials-page">
    <div className="segmented financial-views" aria-label="财报内容">
      <button aria-pressed={view === 'statements'} onClick={() => setView('statements')}>财务报表</button>
      <button aria-pressed={view === 'annual'} onClick={() => setView('annual')}>定期报告</button>
    </div>
    {view === 'statements' ? <FinancialStatements security={security} /> : <AnnualReports security={security} />}
  </div>
}
