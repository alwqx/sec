import { useCallback, useState, type FormEvent } from 'react'
import { ipoAnnouncements } from '../api'
import { useResource } from '../hooks/useResource'
import { securityFromCode } from '../security'
import type { BasicSecurity } from '../types'
import Pagination from './Pagination'
import ResourceState from './ResourceState'

// 东方财富的新股日历接口已下线，这里按公告日期列出巨潮的 IPO 相关公告，不是申购日历。
export default function IPOAnnouncements({ onSelect }: { onSelect: (security: BasicSecurity) => void }) {
  const [range, setRange] = useState({ since: '', until: '' })
  const [draft, setDraft] = useState(range)
  const [error, setError] = useState('')
  const [openError, setOpenError] = useState('')
  const [page, setPage] = useState(1)
  const load = useCallback((signal: AbortSignal) => ipoAnnouncements(page, range.since, range.until, signal), [page, range])
  const resource = useResource(load)
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (draft.since && draft.until && draft.since > draft.until) { setError('开始日期不能晚于结束日期'); return }
    setError('')
    setOpenError('')
    setPage(1)
    setRange({ ...draft })
  }
  const reset = () => {
    const empty = { since: '', until: '' }
    setError('')
    setOpenError('')
    setDraft(empty)
    setPage(1)
    setRange(empty)
  }
  return <>
    <form className="section-toolbar prospectus-search" onSubmit={submit}>
      <label>公告开始日期<input type="date" aria-label="公告开始日期" value={draft.since} onChange={event => setDraft({ ...draft, since: event.target.value })} /></label>
      <label>公告结束日期<input type="date" aria-label="公告结束日期" value={draft.until} onChange={event => setDraft({ ...draft, until: event.target.value })} /></label>
      <button type="submit">查询</button><button type="button" onClick={reset}>重置</button>
    </form>
    {error && <p className="error" role="alert">{error}</p>}
    <p className="data-note">巨潮资讯 · 首次公开发行及上市相关公告，按公告日期排列。公告日期不是申购日或上市日，不能当作准确的申购日历。</p>
    {openError && <p className="error" role="alert">{openError}</p>}
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && (resource.data.results.length === 0 ? <div className="empty-state">暂无符合条件的公告</div> : <div className="table-scroll ipo-announcements"><table className="data-table"><thead><tr><th>公告日期</th><th>证券</th><th>公告标题</th><th>原文</th></tr></thead>
      <tbody>{resource.data.results.map(item => { const security = securityFromCode(item.code, item.name); return <tr key={item.id}>
        <td>{item.date || '—'}</td>
        <td>{security ? <button className="text-link" onClick={() => onSelect(security)}>{item.name}</button> : item.name}<div className="data-note">{item.code}</div></td>
        <td className="ipo-announcement-title">{item.title}</td>
        <td>{item.url ? <a className="text-link" href={item.url} target="_blank" rel="noreferrer" onClick={event => {
          setOpenError('')
          if (!window.secOpenDisclosure) return
          event.preventDefault()
          void window.secOpenDisclosure(item.url).catch(() => setOpenError('打开原文失败，请到巨潮资讯查看。'))
        }}>查看原文 ↗</a> : '—'}</td>
      </tr> })}</tbody></table></div>)}
    {resource.data && <Pagination page={page} total={resource.data.total} hasMore={resource.data.has_more} loading={resource.loading} onPage={next => { setOpenError(''); setPage(next) }} />}
  </>
}
