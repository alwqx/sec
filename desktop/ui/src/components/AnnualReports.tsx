import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { annualReports, downloadAnnualReport } from '../api'
import { useResource } from '../hooks/useResource'
import type { BasicSecurity, AnnualReport, SavedDownload as SavedFile } from '../types'
import ResourceState from './ResourceState'
import SavedDownload from './SavedDownload'

function Results({ code, startYear, endYear }: { code: string; startYear: number; endYear: number }) {
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState('')
  const [error, setError] = useState('')
  const [files, setFiles] = useState<SavedFile[]>([])
  const controller = useRef<AbortController | null>(null)
  const load = useCallback((signal: AbortSignal) => annualReports(code, startYear, endYear, page, signal), [code, startYear, endYear, page])
  const resource = useResource(load)
  useEffect(() => () => controller.current?.abort(), [])
  const rows = resource.data?.results ?? []
  const chosen = rows.filter(item => selected.includes(item.url))
  const download = async (documents: AnnualReport[]) => {
    if (controller.current && !controller.current.signal.aborted) return
    if (!documents.length) return
    const request = new AbortController()
    controller.current = request
    setBusy(true)
    setFiles([])
    setError('')
    let complete = 0
    let failed = 0
    for (let i = 0; i < documents.length; i++) {
      if (request.signal.aborted) break
      setProgress(`正在下载 ${i + 1}/${documents.length}：${documents[i].title}`)
      try {
        const file = await downloadAnnualReport(code, documents[i].year, documents[i].url, request.signal)
        if (!request.signal.aborted) { complete++; setFiles(current => [...current, file]) }
      } catch (e) {
        if (!request.signal.aborted) { failed++; setError(current => [current, `${documents[i].title}：${e instanceof Error ? e.message : String(e)}`].filter(Boolean).join('；')) }
      }
    }
    if (!request.signal.aborted) {
      setProgress(`下载完成：成功 ${complete} 份，失败 ${failed} 份${failed ? '；可重新选择失败文件重试' : ''}`)
      controller.current = null
      setBusy(false)
    }
  }
  const cancel = () => { controller.current?.abort(); setBusy(false); setProgress('下载已取消，已保存文件保留。') }
  const changePage = (next: number) => { setSelected([]); setPage(next) }
  const reload = () => { setSelected([]); resource.reload() }
  return <div>
    <p className="data-note">{startYear} 至 {endYear} 报告年度 · 按报告年度筛选正文，例如 2025 年报通常于 2026 年披露；排除摘要、英文、更正与修订版。选择范围限当前页，翻页后重新选择。文件保存到设置中的下载目录（默认 ~/Downloads），同名自动编号。</p>
    <div className="section-toolbar">
      <label><input type="checkbox" checked={rows.length > 0 && chosen.length === rows.length} disabled={busy || resource.loading || !rows.length} onChange={e => setSelected(e.target.checked ? rows.map(item => item.url) : [])} /> 全选本页</label>
      <button disabled={busy || !chosen.length || resource.loading} onClick={() => void download(chosen)}>下载所选（{chosen.length}）</button>
      <button disabled={busy || resource.loading} onClick={reload}>刷新列表</button>
      {busy && <button onClick={cancel}>取消下载</button>}
    </div>
    {progress && <p className="data-note" role="status">{progress}</p>}
    {error && <p className="error" role="alert">{error}</p>}
    {files.map(file => <SavedDownload key={file.path} file={file} />)}
    <ResourceState {...resource} retry={reload} />
    {resource.data && (rows.length ? <ul className="announcement-list">{rows.map(item => <li key={item.url}>
      <div className="announcement-meta"><time>{item.date || '日期未知'}</time><span>{item.year} 报告年度</span></div>
      <h3><label><input type="checkbox" aria-label={`选择${item.title}`} checked={selected.includes(item.url)} disabled={busy} onChange={e => setSelected(current => e.target.checked ? [...current, item.url] : current.filter(url => url !== item.url))} /> {item.title}</label></h3>
      <div className="announcement-actions"><button disabled={busy} onClick={() => void download([item])}>下载 PDF</button><a href={item.url} target="_blank" rel="noopener noreferrer" onClick={event => {
        if (!window.secOpenDisclosure) return
        event.preventDefault()
        void window.secOpenDisclosure(item.url).catch(() => setError('原文打开失败，请尝试下载 PDF。'))
      }}>查看原文 ↗</a></div>
    </li>)}</ul> : <div className="empty-state">本页暂无符合条件的年度报告正文。{resource.data.has_more ? '可继续查看下一页。' : '可尝试调整报告年度。'}</div>)}
    {resource.data && <div className="pagination"><span>第 {page} 页 · 本页 {rows.length} 份 PDF</span><div><button disabled={busy || resource.loading || page === 1} onClick={() => changePage(page - 1)}>上一页</button><button disabled={busy || resource.loading || !resource.data.has_more} onClick={() => changePage(page + 1)}>下一页</button></div></div>}
  </div>
}

export default function AnnualReports({ security }: { security: BasicSecurity }) {
  const latestYear = Number(new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', year: 'numeric' }).format(new Date())) - 1
  const [draft, setDraft] = useState({ startYear: latestYear, endYear: latestYear })
  const [range, setRange] = useState(draft)
  const [revision, setRevision] = useState(0)
  const [error, setError] = useState('')
  const years = Array.from({ length: latestYear - 1990 + 1 }, (_, i) => latestYear - i)
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (draft.startYear > draft.endYear) { setError('开始年度不能晚于结束年度'); return }
    setError('')
    setRange(draft)
    setRevision(current => current + 1)
  }
  return <section>
    <form className="section-toolbar" onSubmit={submit}>
      <label>开始年度<select aria-label="年报开始年度" value={draft.startYear} onChange={e => setDraft({ ...draft, startYear: Number(e.target.value) })}>{years.map(year => <option key={year} value={year}>{year} 年</option>)}</select></label>
      <label>结束年度<select aria-label="年报结束年度" value={draft.endYear} onChange={e => setDraft({ ...draft, endYear: Number(e.target.value) })}>{years.map(year => <option key={year} value={year}>{year} 年</option>)}</select></label>
      <button type="submit">查询</button>
    </form>
    <p className="data-note">选择相同起止年份可查询单年度。重新查询、切换栏目或离开页面会取消未完成下载，已保存文件保留。</p>
    {error && <p className="error" role="alert">{error}</p>}
    <Results key={`${security.ex_code}-${revision}`} code={security.ex_code} {...range} />
  </section>
}
