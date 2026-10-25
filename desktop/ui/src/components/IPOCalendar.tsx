import { useCallback, useState } from 'react'
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
  const [page, setPage] = useState(1)
  const [view, setView] = useState<'list' | 'prospectus'>('list')
  const [prospectusCode, setProspectusCode] = useState('')
  const load = useCallback((signal: AbortSignal) => ipo(page, signal), [page])
  const resource = useResource(load)
  const select = (item: IPOListing) => { const security = asSecurity(item); if (security) onSelect(security) }
  return <section className="page-panel">
    <div className="section-toolbar"><div className="segmented" aria-label="IPO 栏目"><button aria-pressed={view === 'list'} onClick={() => setView('list')}>新股上市</button><button aria-pressed={view === 'prospectus'} onClick={() => setView('prospectus')}>招股说明书</button></div>{view !== 'prospectus' && <button onClick={resource.reload} disabled={resource.loading}>刷新</button>}</div>
    {view === 'prospectus' ? <Prospectuses key={prospectusCode} initialCode={prospectusCode} /> : <>
    <p className="data-note">东方财富 · 按上市日期排列，支持分页查看；可进入证券研究或查询招股说明书。</p>
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && (resource.data.results.length === 0 ? <div className="empty-state">暂无新股数据</div> : <div className="table-scroll"><table className="data-table"><thead><tr><th>证券名称</th><th>代码</th><th>上市日期</th><th>操作</th></tr></thead><tbody>{resource.data.results.map((item, i) => <tr key={`${item.code}-${i}`}><td>{item.name}</td><td>{item.code}</td><td>{listingDate(item)}</td><td><button onClick={() => select(item)} disabled={!asSecurity(item)}>查看证券</button><button disabled={!asSecurity(item)} onClick={() => { setProspectusCode(item.code); setView('prospectus') }}>招股书</button></td></tr>)}</tbody></table></div>)}
    {resource.data && <Pagination page={page} total={resource.data.total} hasMore={resource.data.has_more} loading={resource.loading} onPage={setPage} />}
    </>}
  </section>
}
