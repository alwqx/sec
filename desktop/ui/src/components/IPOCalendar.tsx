import { useCallback, useState } from 'react'
import { ipo } from '../api'
import { useResource } from '../hooks/useResource'
import type { BasicSecurity, IPOListing } from '../types'
import ResourceState from './ResourceState'
import Pagination from './Pagination'

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
  const [view, setView] = useState<'calendar' | 'list'>('calendar')
  const load = useCallback((signal: AbortSignal) => ipo(page, signal), [page])
  const resource = useResource(load)
  const groups = new Map<string, IPOListing[]>()
  resource.data?.results.forEach(item => {
    const date = listingDate(item)
    groups.set(date, [...(groups.get(date) ?? []), item])
  })
  const select = (item: IPOListing) => { const security = asSecurity(item); if (security) onSelect(security) }
  return <section className="page-panel">
    <div className="section-toolbar"><div className="segmented" aria-label="新股展示方式"><button aria-pressed={view === 'calendar'} onClick={() => setView('calendar')}>上市排期</button><button aria-pressed={view === 'list'} onClick={() => setView('list')}>新股列表</button></div><button onClick={resource.reload} disabled={resource.loading}>刷新</button></div>
    <p className="data-note">东方财富新股列表 · 按上市日期排列。排期按当前页分组，申购日期暂未提供；点击证券进入研究工作台。</p>
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && (resource.data.results.length === 0 ? <div className="empty-state">暂无新股数据</div> : view === 'calendar'
      ? <div className="ipo-timeline">{Array.from(groups).map(([date, items]) => <section className="ipo-day" key={date}><h2>{date}</h2><div>{items.map((item, i) => <button className="ipo-security" key={`${item.code}-${i}`} onClick={() => select(item)} disabled={!asSecurity(item)}><span>{item.name}</span><span className="data-note">{item.code}</span><span className="text-link">查看证券 →</span></button>)}</div></section>)}</div>
      : <div className="table-scroll"><table className="data-table"><thead><tr><th>证券名称</th><th>代码</th><th>上市日期</th><th>操作</th></tr></thead><tbody>{resource.data.results.map((item, i) => <tr key={`${item.code}-${i}`}><td>{item.name}</td><td>{item.code}</td><td>{listingDate(item)}</td><td><button onClick={() => select(item)} disabled={!asSecurity(item)}>查看证券</button></td></tr>)}</tbody></table></div>)}
    {resource.data && <Pagination page={page} total={resource.data.total} hasMore={resource.data.has_more} loading={resource.loading} onPage={setPage} />}
  </section>
}
