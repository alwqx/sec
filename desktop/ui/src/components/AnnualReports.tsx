import { useCallback, useEffect, useRef, useState } from 'react'
import { annualReports, downloadAnnualReport } from '../api'
import { useResource } from '../hooks/useResource'
import type { BasicSecurity, SavedDownload as SavedFile, AnnualReport } from '../types'
import ResourceState from './ResourceState'
import SavedDownload from './SavedDownload'

export default function AnnualReports({ security }: { security: BasicSecurity }) {
  const latestYear = Number(new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', year: 'numeric' }).format(new Date())) - 1
  const [year, setYear] = useState(latestYear)
  const [page, setPage] = useState(1)
  const [downloading, setDownloading] = useState('')
  const [saved, setSaved] = useState<SavedFile | null>(null)
  const [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  const load = useCallback((signal: AbortSignal) => annualReports(security.ex_code, year, page, signal), [security.ex_code, year, page])
  const resource = useResource(load)
  useEffect(() => {
    setDownloading('')
    setError('')
    return () => controller.current?.abort()
  }, [security.ex_code, year, page])
  const download = async (report: AnnualReport) => {
    if (controller.current && !controller.current.signal.aborted && downloading) return
    const request = new AbortController()
    controller.current = request
    setDownloading(report.url)
    setSaved(null)
    setError('')
    try {
      const file = await downloadAnnualReport(security.ex_code, report.year, report.url, request.signal)
      if (!request.signal.aborted) setSaved(file)
    } catch (e) {
      if (!request.signal.aborted) setError(e instanceof Error ? e.message : String(e))
    } finally {
      if (!request.signal.aborted) setDownloading('')
    }
  }
  const cancel = () => { controller.current?.abort(); setDownloading(''); setError('下载已取消') }
  return <section>
    <div className="section-toolbar"><label>报告年度<select aria-label="年报报告年度" value={year} onChange={e => { setPage(1); setYear(Number(e.target.value)) }}>{Array.from({ length: latestYear - 1990 + 1 }, (_, i) => latestYear - i).map(y => <option key={y} value={y}>{y} 年</option>)}</select></label><button disabled={resource.loading} onClick={resource.reload}>刷新列表</button></div>
    <p className="data-note">按报告年度查询，例如 2025 年报通常于 2026 年披露。仅展示年度报告正文，排除摘要、英文版、更正与修订版。文件保存到设置中的下载目录（默认 ~/Downloads），同名文件自动编号。</p>
    {error && <p className="error" role="alert">{error}</p>}
    {saved && <SavedDownload file={saved} />}
    {downloading && <div className="download-status" role="status">正在下载 PDF…<button onClick={cancel}>取消下载</button></div>}
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && (resource.data.results.length === 0 ? <div className="empty-state">当前页暂无符合条件的年度报告正文。{resource.data.has_more ? '可继续查看下一页。' : ''}</div> : <ul className="announcement-list">{resource.data.results.map((report, index) => <li key={`${report.id}-${index}`}>
      <div className="announcement-meta"><time>{report.date || '日期未知'}</time><span>{report.year} 报告年度</span></div><h3>{report.title}</h3>
      <div className="announcement-actions"><button className="primary-button" disabled={!!downloading} onClick={() => void download(report)}>{downloading === report.url ? '下载中…' : '下载 PDF'}</button><a href={report.url} target="_blank" rel="noopener noreferrer" onClick={event => {
        if (!window.secOpenDisclosure) return
        event.preventDefault()
        void window.secOpenDisclosure(report.url).catch(() => setError('原文打开失败，请尝试下载 PDF。'))
      }}>查看原文 ↗</a></div>
    </li>)}</ul>)}
    {resource.data && <div className="pagination"><span>第 {page} 页 · 本页 {resource.data.results.length} 份正文</span><div><button disabled={resource.loading || page === 1} onClick={() => setPage(page - 1)}>上一页</button><button disabled={resource.loading || !resource.data.has_more} onClick={() => setPage(page + 1)}>下一页</button></div></div>}
  </section>
}
