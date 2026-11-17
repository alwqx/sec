import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { downloadProspectus, prospectuses } from '../api'
import { useAIModels } from '../hooks/useAIModels'
import { useResource } from '../hooks/useResource'
import type { ProspectusDocument, SavedDownload as SavedFile } from '../types'
import ProspectusAsk from './ProspectusAsk'
import ResourceState from './ResourceState'
import SavedDownload from './SavedDownload'
import SummaryModelPicker from './SummaryModelPicker'

function Results({ code, begin, end }: { code: string; begin: string; end: string }) {
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState('')
  const [error, setError] = useState('')
  const [files, setFiles] = useState<SavedFile[]>([])
  const controller = useRef<AbortController | null>(null)
  const load = useCallback((signal: AbortSignal) => prospectuses(code, begin, end, page, signal), [code, begin, end, page])
  const resource = useResource(load)
  useEffect(() => () => controller.current?.abort(), [])
  const rows = resource.data?.results ?? []
  const chosen = rows.filter(item => selected.includes(item.url))
  // 没有出现过招股说明书、招股意向书全文时不读取模型配置。出现过以后一直读取：翻页、刷新时列表会短暂为空，
  // 不能因此重新请求模型配置和系统钥匙串。
  const askable = rows.some(item => item.askable)
  const seenAskable = useRef(false)
  if (askable) seenAskable.current = true
  const models = useAIModels(seenAskable.current)
  const { profile, unavailable } = models
  // asking 是正在回答问题的行数，回答时禁用刷新和翻页（卸载行会取消回答）。
  const [asking, setAsking] = useState(0)
  const onAskBusy = useCallback((busy: boolean) => setAsking(current => current + (busy ? 1 : -1)), [])
  const locked = busy || asking > 0
  const lockedTitle = asking > 0 ? '正在回答问题，完成或取消后再刷新、翻页' : undefined
  const download = async (documents: ProspectusDocument[]) => {
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
        const file = await downloadProspectus(code, documents[i].url, request.signal)
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
    <p className="data-note">{resource.data ? `${resource.data.name} · ${resource.data.code} · ` : ''}巨潮资讯「首次公开发行及上市」公告，包含招股说明书、招股意向书与发行公告。仅展示有效 PDF；选择范围限当前页。保存到设置中的下载目录（默认 ~/Downloads），同名自动编号。</p>
    <div className="section-toolbar">
      <label><input type="checkbox" checked={rows.length > 0 && chosen.length === rows.length} disabled={busy || resource.loading || !rows.length} onChange={e => setSelected(e.target.checked ? rows.map(item => item.url) : [])} /> 全选本页</label>
      <button disabled={busy || !chosen.length || resource.loading} onClick={() => void download(chosen)}>下载所选（{chosen.length}）</button>
      <button disabled={locked || resource.loading} title={lockedTitle} onClick={reload}>刷新列表</button>
      {busy && <button onClick={cancel}>取消下载</button>}
    </div>
    {progress && <p className="data-note" role="status">{progress}</p>}
    {error && <p className="error" role="alert">{error}</p>}
    {files.map(file => <SavedDownload key={file.path} file={file} />)}
    {askable && <SummaryModelPicker models={models} action="提问" sends="问题和按问题挑出的招股书页面文本" extra="只有招股说明书、招股意向书全文可以提问，回答附物理 PDF 页码；扫描件暂不支持。" />}
    <ResourceState {...resource} retry={reload} />
    {resource.data && (rows.length ? <ul className="announcement-list">{rows.map(item => <li key={item.url}>
      <div className="announcement-meta"><time>{item.date || '日期未知'}</time></div>
      <h3><label><input type="checkbox" aria-label={`选择${item.title}`} checked={selected.includes(item.url)} disabled={busy} onChange={e => setSelected(current => e.target.checked ? [...current, item.url] : current.filter(url => url !== item.url))} /> {item.title}</label></h3>
      <div className="announcement-actions"><button disabled={busy} onClick={() => void download([item])}>下载 PDF</button><a href={item.url} target="_blank" rel="noopener noreferrer" onClick={event => {
        if (!window.secOpenDisclosure) return
        event.preventDefault()
        void window.secOpenDisclosure(item.url).catch(() => setError('原文打开失败，请尝试下载 PDF。'))
      }}>查看原文 ↗</a></div>
      {item.askable && <ProspectusAsk url={item.url} profile={profile} unavailable={unavailable} onBusy={onAskBusy} />}
    </li>)}</ul> : <div className="empty-state">本页暂无有效招股说明书或 IPO 相关 PDF。{resource.data.has_more ? '可继续查看下一页。' : '可尝试扩大日期范围或确认证券代码。'}</div>)}
    {resource.data && <div className="pagination"><span>第 {page} 页 · 本页 {rows.length} 份 PDF</span><div><button disabled={locked || resource.loading || page === 1} title={lockedTitle} onClick={() => changePage(page - 1)}>上一页</button><button disabled={locked || resource.loading || !resource.data.has_more} title={lockedTitle} onClick={() => changePage(page + 1)}>下一页</button></div></div>}
  </div>
}

export default function Prospectuses({ initialCode = '' }: { initialCode?: string }) {
  const [draft, setDraft] = useState({ code: initialCode, begin: '', end: '' })
  const [query, setQuery] = useState(initialCode ? draft : null)
  const [revision, setRevision] = useState(0)
  const [error, setError] = useState('')
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const code = draft.code.trim().toUpperCase()
    if (!/^(?:(?:SH|SZ|BJ))?\d{6}$/.test(code)) { setError('请输入六位 A 股代码，可带 SH/SZ/BJ 前缀'); return }
    if (draft.begin && draft.end && draft.begin > draft.end) { setError('开始日期不能晚于结束日期'); return }
    setError('')
    setQuery({ ...draft, code })
    setRevision(current => current + 1)
  }
  return <section>
    <form className="section-toolbar prospectus-search" onSubmit={submit}>
      <label>证券代码<input aria-label="招股说明书证券代码" value={draft.code} onChange={e => setDraft({ ...draft, code: e.target.value })} placeholder="如 300750" maxLength={8} required /></label>
      <label>公告开始日期<input type="date" aria-label="招股说明书公告开始日期" min="1990-01-01" value={draft.begin} onChange={e => setDraft({ ...draft, begin: e.target.value })} /></label>
      <label>公告结束日期<input type="date" aria-label="招股说明书公告结束日期" min="1990-01-01" value={draft.end} onChange={e => setDraft({ ...draft, end: e.target.value })} /></label>
      <button type="submit">查询</button>
    </form>
    <p className="data-note">支持沪深北 A 股，日期可留空查询全部历史。重新查询、切换栏目或离开页面将取消未完成的下载。</p>
    {error && <p className="error" role="alert">{error}</p>}
    {query ? <Results key={revision} {...query} /> : <div className="empty-state">输入证券代码查询招股说明书，或从新股列表选择「招股书」。</div>}
  </section>
}
