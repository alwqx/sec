import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { loadWorkspace, saveWorkspace, search } from './api'
import type { BasicSecurity, WorkspaceSettings, WorkspaceState } from './types'
import SearchBar from './components/SearchBar'
import SecurityList from './components/SecurityList'
import SecurityDetails from './components/SecurityDetails'
import Overview from './components/Overview'
import Financials from './components/Financials'
import IPOCalendar from './components/IPOCalendar'
import Announcements from './components/Announcements'
import Settings from './components/Settings'

type Page = 'stocks' | 'ipo' | 'announcements' | 'settings'
type Tab = 'overview' | 'company' | 'financials' | 'dividends' | 'announcements'
const pages: { id: Page; title: string; icon: string; description: string }[] = [
  { id: 'stocks', title: '自选与行情', icon: '↗', description: '从市场行情到公司研究，在同一个工作区完成' },
  { id: 'ipo', title: 'IPO 日历', icon: '▦', description: '追踪新股上市排期与最新上市证券' },
  { id: 'announcements', title: '公告中心', icon: '▤', description: '查询巨潮资讯最新披露与证券公告' },
  { id: 'settings', title: '设置', icon: '⚙', description: '管理工作台外观与行情刷新偏好' },
]
const tabs: { id: Tab; title: string }[] = [
  { id: 'overview', title: '概览' }, { id: 'company', title: '公司' }, { id: 'financials', title: '财报' },
  { id: 'dividends', title: '分红' }, { id: 'announcements', title: '公告' },
]
const initialWorkspace: WorkspaceState = { watchlist: [], settings: { refresh_seconds: 5, theme: 'dark' } }

export default function App() {
  const [page, setPage] = useState<Page>('stocks')
  const [tab, setTab] = useState<Tab>('overview')
  const [announcementScope, setAnnouncementScope] = useState<'latest' | 'selected'>('latest')
  const [listMode, setListMode] = useState<'watchlist' | 'search'>('watchlist')
  const [results, setResults] = useState<BasicSecurity[]>([])
  const [selected, setSelected] = useState<BasicSecurity | null>(null)
  const [searching, setSearching] = useState(false)
  const [searchedKey, setSearchedKey] = useState('')
  const [searchError, setSearchError] = useState('')
  const [workspace, setWorkspace] = useState(initialWorkspace)
  const [ready, setReady] = useState(false)
  const [storeError, setStoreError] = useState('')
  const [saving, setSaving] = useState(false)
  const [storeRetry, setStoreRetry] = useState(0)
  const searchController = useRef<AbortController | null>(null)
  const savingRef = useRef(false)

  useEffect(() => {
    const controller = new AbortController()
    setStoreError('')
    void loadWorkspace(controller.signal).then(state => {
      if (controller.signal.aborted) return
      setWorkspace(state)
      setSelected(current => current ?? state.watchlist[0] ?? null)
      setReady(true)
    }).catch((e: unknown) => {
      if (!controller.signal.aborted) setStoreError(e instanceof Error ? e.message : String(e))
    })
    return () => controller.abort()
  }, [storeRetry])

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const apply = () => { document.documentElement.dataset.theme = workspace.settings.theme === 'system' ? (media.matches ? 'dark' : 'light') : workspace.settings.theme }
    apply()
    media.addEventListener('change', apply)
    return () => media.removeEventListener('change', apply)
  }, [workspace.settings.theme])

  useEffect(() => () => searchController.current?.abort(), [])

  const persist = async (state: WorkspaceState) => {
    if (!ready || savingRef.current) return false
    savingRef.current = true
    setSaving(true)
    setStoreError('')
    try { setWorkspace(await saveWorkspace(state)); return true }
    catch (e) { setStoreError(e instanceof Error ? e.message : String(e)); return false }
    finally { savingRef.current = false; setSaving(false) }
  }

  const onSearch = useCallback(async (key: string) => {
    searchController.current?.abort()
    const controller = new AbortController()
    searchController.current = controller
    setSearching(true)
    setSearchError('')
    setSearchedKey(key)
    setPage('stocks')
    setListMode('search')
    setResults([])
    try {
      const items = await search(key, controller.signal)
      if (!controller.signal.aborted) setResults(items)
    } catch (e) {
      if (!controller.signal.aborted) setSearchError(e instanceof Error ? e.message : String(e))
    } finally {
      if (!controller.signal.aborted) setSearching(false)
    }
  }, [])

  const onSelect = (security: BasicSecurity) => { setSelected(security) }
  const openIPO = (security: BasicSecurity) => {
    setSelected(security)
    setTab('overview')
    setPage('stocks')
  }
  const watched = selected !== null && workspace.watchlist.some(item => item.ex_code === selected.ex_code)
  const toggleWatch = () => {
    if (!selected) return
    if (!watched && workspace.watchlist.length >= 500) { setStoreError('最多保存 500 只自选证券'); return }
    void persist({ ...workspace, watchlist: watched ? workspace.watchlist.filter(item => item.ex_code !== selected.ex_code) : [...workspace.watchlist, selected] })
  }
  const saveSettings = (settings: WorkspaceSettings) => persist({ ...workspace, settings })
  const currentPage = pages.find(item => item.id === page)!
  const items = listMode === 'watchlist' ? workspace.watchlist : results
  const aShare = selected?.security_type === 'stock' && /^(SH|SZ|BJ)\d{6}$/i.test(selected.ex_code)

  const tabKey = (e: KeyboardEvent<HTMLButtonElement>, index: number) => {
    const direction = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0
    if (!direction && e.key !== 'Home' && e.key !== 'End') return
    e.preventDefault()
    const next = e.key === 'Home' ? 0 : e.key === 'End' ? tabs.length - 1 : (index + direction + tabs.length) % tabs.length
    setTab(tabs[next].id)
    document.getElementById(`tab-${tabs[next].id}`)?.focus()
  }

  return <div className="app">
    <header className="topbar">
      <div className="brand"><span className="brand-mark">s</span><div><strong>sec</strong><span>研究工作台</span></div></div>
      <SearchBar onSearch={onSearch} loading={searching} />
      <span className="topbar-caption">A 股 · 港股 · 美股</span>
    </header>
    <div className="app-shell">
      <aside className="sidebar">
        <span className="nav-group-label">工作空间</span>
        <nav className="side-nav" aria-label="主导航">{pages.map(item => <button key={item.id}
          className={item.id === 'settings' ? 'settings-nav' : ''}
          aria-current={page === item.id ? 'page' : undefined} onClick={() => setPage(item.id)}>
          <span className="nav-icon" aria-hidden="true">{item.icon}</span>{item.title}
        </button>)}</nav>
        <div className="sidebar-footer"><span className="status-dot" />本地工作台<span>自选 · 行情 · 研究</span></div>
      </aside>
      <main className="main-body" id="main-content">
        <header className="app-header"><div><h1>{currentPage.title}</h1><p>{currentPage.description}</p></div>
          {selected && page !== 'stocks' && <button className="context-security" onClick={() => setPage('stocks')}>{selected.name} · 返回研究 →</button>}
        </header>
        {storeError && <div className="error" role="alert">{storeError}{!ready && <button onClick={() => setStoreRetry(n => n + 1)}>重新加载</button>}</div>}
        {!ready && !storeError && <div className="data-note" role="status">正在读取本地自选与设置…</div>}
        {page === 'stocks' && <div className="workspace-grid">
          <section className="security-browser" aria-label="证券列表">
            <div className="browser-tabs segmented"><button aria-pressed={listMode === 'watchlist'} onClick={() => setListMode('watchlist')}>我的自选 <span>{workspace.watchlist.length}</span></button><button aria-pressed={listMode === 'search'} onClick={() => setListMode('search')}>搜索结果</button></div>
            <div className="results-caption">{listMode === 'watchlist' ? '关注的证券，随时继续研究' : searchedKey ? `“${searchedKey}” · ${results.length} 个结果` : '在顶部输入名称或代码'}</div>
            {listMode === 'search' && searchError && <div className="error" role="alert">{searchError}</div>}
            {listMode === 'search' && searching ? <div className="empty-state" role="status">正在搜索证券…</div>
              : <SecurityList items={items} selected={selected} onSelect={onSelect} emptyText={listMode === 'watchlist' ? '暂无自选证券。搜索并选中证券后，点击“加入自选”。' : searchedKey ? '未找到证券，请尝试其他名称或代码' : '输入关键字搜索证券'} />}
          </section>
          <section className="research-workspace" aria-label="个股研究">
            {selected ? <>
              <header className="security-header"><div><div className="eyebrow">证券研究</div><h2>{selected.name}<span>{selected.ex_code}</span></h2></div><button className={watched ? 'watch-button is-watched' : 'watch-button'} aria-pressed={watched} aria-label={watched ? `将${selected.name}移出自选` : `将${selected.name}加入自选`} title={watched ? '点击移出自选' : '点击加入自选'} disabled={!ready || saving} onClick={toggleWatch}>{saving ? '保存中…' : watched ? '★ 已加入自选' : '☆ 加入自选'}</button></header>
              <div className="research-tabs" role="tablist" aria-label="个股研究栏目">{tabs.map((item, index) => <button key={item.id} id={`tab-${item.id}`} role="tab" aria-controls="research-panel" aria-selected={tab === item.id} tabIndex={tab === item.id ? 0 : -1} onKeyDown={e => tabKey(e, index)} onClick={() => setTab(item.id)}>{item.title}</button>)}</div>
              <div className="research-panel" role="tabpanel" id="research-panel" aria-labelledby={`tab-${tab}`} key={`${selected.ex_code}-${tab}`}>
                {tab === 'overview' ? <Overview security={selected} interval={workspace.settings.refresh_seconds} />
                  : !aShare ? <div className="empty-state">此栏目暂仅支持沪深北 A 股，当前证券可在概览中查看行情。</div>
                  : tab === 'financials' ? <Financials security={selected} />
                  : tab === 'announcements' ? <Announcements code={selected.ex_code} compact />
                  : <SecurityDetails security={selected} tab={tab} />}
              </div>
            </> : <div className="welcome-state"><div className="welcome-symbol" aria-hidden="true">↗</div><h2>从一只证券开始研究</h2><p>搜索证券，或从自选列表中选择。<br />行情、公司资料、财报与公告都在这里。</p><div className="welcome-examples">试试 <button onClick={() => void onSearch('招商银行')}>招商银行</button><button onClick={() => void onSearch('腾讯控股')}>腾讯控股</button><button onClick={() => void onSearch('AAPL')}>AAPL</button></div></div>}
          </section>
        </div>}
        {page === 'ipo' && <IPOCalendar onSelect={openIPO} />}
        {page === 'announcements' && <div className="global-announcements"><div className="scope-bar"><span>公告范围</span><button aria-pressed={announcementScope === 'latest' || !aShare} onClick={() => setAnnouncementScope('latest')}>最新披露</button><button disabled={!aShare} aria-pressed={announcementScope === 'selected' && !!aShare} onClick={() => setAnnouncementScope('selected')}>{selected ? selected.name : '当前证券'}</button></div><Announcements key={announcementScope === 'selected' && aShare ? selected!.ex_code : 'latest'} code={announcementScope === 'selected' && aShare ? selected!.ex_code : ''} /></div>}
        {page === 'settings' && (ready ? <Settings settings={workspace.settings} saving={saving} onSave={saveSettings} /> : <div className="page-panel empty-state">读取本地数据后可修改设置</div>)}
      </main>
    </div>
  </div>
}
