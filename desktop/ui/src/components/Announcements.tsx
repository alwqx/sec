import { useCallback, useState, type FormEvent } from 'react'
import { announcements } from '../api'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'
import Pagination from './Pagination'

export default function Announcements({ code = '', compact = false }: { code?: string; compact?: boolean }) {
  const [page, setPage] = useState(1)
  const [draft, setDraft] = useState('')
  const [key, setKey] = useState('')
  const [copied, setCopied] = useState('')
  const [copyError, setCopyError] = useState('')
  const load = useCallback((signal: AbortSignal) => announcements(page, code, key, signal), [page, code, key])
  const resource = useResource(load)
  const submit = (e: FormEvent) => { e.preventDefault(); setPage(1); setKey(draft.trim()) }
  const copy = async (url: string) => {
    try { await navigator.clipboard.writeText(url); setCopied(url); setCopyError('') }
    catch { setCopyError('复制失败，请使用原文链接打开或复制链接地址。') }
  }
  return <section className={compact ? 'research-section' : 'page-panel'}>
    <form className="section-toolbar announcement-search" onSubmit={submit}><label className="visually-hidden" htmlFor={compact ? 'stock-ann-key' : 'ann-key'}>公告关键字</label><input id={compact ? 'stock-ann-key' : 'ann-key'} value={draft} onChange={e => setDraft(e.target.value)} placeholder="搜索公告标题" maxLength={100} /><button type="submit" disabled={resource.loading}>查询</button><button type="button" onClick={resource.reload} disabled={resource.loading}>刷新</button></form>
    <p className="data-note">巨潮资讯 · {code ? '当前证券公告' : '最新披露公告'} · 原文可打开或复制链接。</p>
    {copyError && <p className="error" role="alert">{copyError}</p>}
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && (resource.data.results.length === 0 ? <div className="empty-state">暂无符合条件的公告</div> : <ul className="announcement-list">{resource.data.results.map((item, i) => <li key={`${item.id}-${i}`}><div className="announcement-meta"><time>{item.date || '日期未知'}</time><span>{item.name} {item.code}</span></div><h3>{item.title}</h3><div className="announcement-actions"><a href={item.url} target="_blank" rel="noopener noreferrer" onClick={e => {
      if (!window.secOpenDisclosure) return
      e.preventDefault()
      void window.secOpenDisclosure(item.url).catch(() => setCopyError('打开原文失败，请复制链接到浏览器。'))
    }}>查看原文 ↗</a><button onClick={() => void copy(item.url)}>{copied === item.url ? '已复制链接' : '复制链接'}</button></div></li>)}</ul>)}
    {resource.data && <Pagination page={page} total={resource.data.total} hasMore={resource.data.has_more} loading={resource.loading} onPage={setPage} />}
  </section>
}
