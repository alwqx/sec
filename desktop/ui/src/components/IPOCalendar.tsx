import { useCallback, useState, type FormEvent } from 'react'
import { ipo } from '../api'
import { useResource } from '../hooks/useResource'
import type { BasicSecurity, IPOListing } from '../types'
import ResourceState from './ResourceState'
import Pagination from './Pagination'
import Prospectuses from './Prospectuses'

function listingDate(item: IPOListing) {
  const value = item.listing_date
  return /^\d{8}$/.test(value) ? `${value.slice(0, 4)}-${value.slice(4, 6)}-${value.slice(6)}` : '日期待定'
}

function asSecurity(item: IPOListing): BasicSecurity | null {
  if (!/^\d{6}$/.test(item.code)) return null
  const exchange = item.code.startsWith('6') ? 'SH' : /^[489]/.test(item.code) ? 'BJ' : 'SZ'
  return { name: item.name, code: item.code, ex_code: exchange + item.code, security_type: 'stock', exchange: { SH: 'sse', SZ: 'szse', BJ: 'bse' }[exchange] }
}

export default function IPOCalendar({ onSelect }: { onSelect: (security: BasicSecurity) => void }) {
  const [range, setRange] = useState({ since: '', until: '' })
  const [draft, setDraft] = useState(range)
  const [error, setError] = useState('')
  const [page, setPage] = useState(1)
  const [view, setView] = useState<'list' | 'prospectus'>('list')
  const [prospectusCode, setProspectusCode] = useState('')
  const load = useCallback((signal: AbortSignal) => ipo(page, range.since, range.until, signal), [page, range.since, range.until])
  const resource = useResource(load)
  const apply = (next: typeof range) => {
    setError(''); setPage(1); setRange(next)
    if (page === 1 && next.since === range.since && next.until === range.until) resource.reload()
  }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (draft.since && draft.until && draft.since > draft.until) { setError('开始日期不能晚于结束日期'); return }
    apply(draft)
  }
  const select = (item: IPOListing) => { const security = asSecurity(item); if (security) onSelect(security) }
  return <section className="page-panel">
    <div className="section-toolbar"><div className="segmented" aria-label="IPO 栏目"><button aria-pressed={view === 'list'} onClick={() => setView('list')}>新股上市</button><button aria-pressed={view === 'prospectus'} onClick={() => setView('prospectus')}>招股说明书</button></div>{view !== 'prospectus' && <button onClick={resource.reload} disabled={resource.loading}>刷新</button>}</div>
    {view === 'prospectus' ? <Prospectuses key={prospectusCode} initialCode={prospectusCode} /> : <>
    <form className="section-toolbar prospectus-search" onSubmit={submit}>
      <label>上市开始日期<input type="date" aria-label="上市开始日期" value={draft.since} onChange={e => setDraft({ ...draft, since: e.target.value })} /></label>
      <label>上市结束日期<input type="date" aria-label="上市结束日期" value={draft.until} onChange={e => setDraft({ ...draft, until: e.target.value })} /></label>
      <button type="submit">查询</button><button type="button" onClick={() => { const empty = { since: '', until: '' }; setDraft(empty); apply(empty) }}>重置</button>
    </form>
    {error && <p className="error" role="alert">{error}</p>}
    <p className="data-note">上市日期：{range.since || '不限开始'} 至 {range.until || '不限结束'}（含起止日）。{(range.since || range.until) && '日期待定记录不纳入筛选结果。'}</p>
    <p className="data-note">东方财富 · 按上市日期排列，支持分页查看；可进入证券研究或查询招股说明书。</p>
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && (resource.data.results.length === 0 ? <div className="empty-state">暂无符合条件的新股数据</div> : <div className="table-scroll"><table className="data-table"><thead><tr><th>证券名称</th><th>代码</th><th>上市日期</th><th>操作</th></tr></thead><tbody>{resource.data.results.map((item, i) => <tr key={`${item.code}-${i}`}><td>{item.name}</td><td>{item.code}</td><td>{listingDate(item)}</td><td><button onClick={() => select(item)} disabled={!asSecurity(item)}>查看证券</button><button disabled={!asSecurity(item)} onClick={() => { setProspectusCode(item.code); setView('prospectus') }}>招股书</button></td></tr>)}</tbody></table></div>)}
    {resource.data && <Pagination page={page} total={resource.data.total} hasMore={resource.data.has_more} loading={resource.loading} onPage={setPage} />}
    </>}
  </section>
}
