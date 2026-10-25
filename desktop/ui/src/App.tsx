import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { ChartCandlestick, FilePlusCorner, Newspaper, Coins, Landmark, Settings as SettingsIcon, RefreshCw, Star, type LucideIcon } from 'lucide-react'
import { loadWorkspace, saveWorkspace, search, loadWatchlist, changeWatchlist } from './api'
import type { BasicSecurity, WorkspaceSettings, WorkspaceState } from './types'
import SearchBar from './components/SearchBar'
import SecurityList from './components/SecurityList'
import SecurityDetails from './components/SecurityDetails'
import Overview from './components/Overview'
import Financials from './components/Financials'
import IPOCalendar from './components/IPOCalendar'
import Announcements from './components/Announcements'
import Settings from './components/Settings'
import Metal from './components/Metal'
import Bond from './components/Bond'
import secLogo from '../../assets/sec-logo.png'

type Page = 'stocks' | 'ipo' | 'announcements' | 'metal' | 'bond' | 'settings'
type Tab = 'overview' | 'company' | 'financials' | 'dividends' | 'announcements'
const pages: { id: Page; title: string; icon: LucideIcon }[] = [
  { id: 'stocks', title: '自选与行情', icon: ChartCandlestick },
  { id: 'ipo', title: 'IPO', icon: FilePlusCorner },
  { id: 'announcements', title: '公告中心', icon: Newspaper },
  { id: 'metal', title: '黄金行情', icon: Coins },
  { id: 'bond', title: '美债收益率', icon: Landmark },
  { id: 'settings', title: '设置', icon: SettingsIcon },
]
const tabs: { id: Tab; title: string }[] = [
  { id: 'overview', title: '概览' }, { id: 'company', title: '公司' }, { id: 'financials', title: '财报' },
  { id: 'dividends', title: '分红' }, { id: 'announcements', title: '公告' },
]
const initialWorkspace: WorkspaceState = { watchlist: [], settings: { refresh_seconds: 5, theme: 'dark', download_directory: '' } }

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
  const pageContentRef = useRef<HTMLDivElement | null>(null)
  const savingRef = useRef(false)
  const watchRevision = useRef(0)
  const [watchRefresh, setWatchRefresh] = useState(0)

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
  useEffect(() => { pageContentRef.current?.scrollTo(0, 0) }, [page])

  useEffect(() => {
    if (!ready || page !== 'stocks') return
    const controller = new AbortController()
    let requestSequence = 0
    const refresh = () => {
      if (savingRef.current) return
      const revision = watchRevision.current
      const sequence = ++requestSequence
      void loadWatchlist(controller.signal).then(watchlist => {
        if (!controller.signal.aborted && !savingRef.current && revision === watchRevision.current && sequence === requestSequence) setWorkspace(current => ({ ...current, watchlist }))
      }).catch((e: unknown) => {
        if (!controller.signal.aborted) setStoreError(e instanceof Error ? e.message : String(e))
      })
    }
    refresh()
    window.addEventListener('focus', refresh)
    return () => { controller.abort(); window.removeEventListener('focus', refresh) }
  }, [ready, page, listMode, watchRefresh])

  const persist = async (state: WorkspaceState) => {
    if (!ready || savingRef.current) return false
    savingRef.current = true
    watchRevision.current++
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
  const watched = selected !== null && workspace.watchlist.some(item => item.ex_code.toUpperCase() === selected.ex_code.toUpperCase())
  const toggleWatch = async () => {
    if (!selected || !ready || savingRef.current) return
    savingRef.current = true
    watchRevision.current++
    setSaving(true)
    setStoreError('')
    try {
      const watchlist = await changeWatchlist(watched ? 'remove' : 'add', selected)
      setWorkspace(current => ({ ...current, watchlist }))
    } catch (e) { setStoreError(e instanceof Error ? e.message : String(e)) }
    finally { savingRef.current = false; setSaving(false) }
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
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand"><img className="brand-mark" src={secLogo} alt="" width={36} height={36} /><strong>SEC</strong></div>
        <span className="nav-group-label">工作空间</span>
        <nav className="side-nav" aria-label="主导航">{pages.map(item => <button key={item.id}
          className={item.id === 'settings' ? 'settings-nav' : ''}
          aria-current={page === item.id ? 'page' : undefined} onClick={() => setPage(item.id)}>
          <item.icon className="nav-icon" size={20} strokeWidth={1.75} aria-hidden="true" focusable="false" />{item.title}
        </button>)}</nav>
        <div className="sidebar-footer"><span className="status-dot" />本地工作台<span>自选 · 行情 · 研究</span></div>
      </aside>
      <main className="main-body" id="main-content">
        <header className="app-header"><div className="page-heading"><currentPage.icon size={22} strokeWidth={1.75} aria-hidden="true" /><h1>{currentPage.title}</h1></div><div className="page-actions" id="page-actions">
          {selected && page !== 'stocks' && <button className="context-security" onClick={() => setPage('stocks')}>{selected.name} · 返回研究 →</button>}
          </div>
        </header>
        <div ref={pageContentRef} className={page === 'stocks' ? 'page-content page-content--workspace' : 'page-content'}>
        {storeError && <div className="error" role="alert">{storeError}{!ready && <button onClick={() => setStoreRetry(n => n + 1)}>重新加载</button>}</div>}
        {!ready && !storeError && <div className="data-note" role="status">正在读取本地自选与设置…</div>}
        {page === 'stocks' && <div className="workspace-grid">
          <section className="security-browser" aria-label="证券列表">
            <div className="browser-search"><SearchBar onSearch={onSearch} loading={searching} initialKey={searchedKey} /></div>
            <div className="browser-toolbar"><div className="browser-tabs segmented"><button aria-pressed={listMode === 'watchlist'} onClick={() => setListMode('watchlist')}>我的自选 <span>{workspace.watchlist.length}</span></button><button aria-pressed={listMode === 'search'} onClick={() => setListMode('search')}>搜索结果</button></div>
            {listMode === 'watchlist' && <button className="watchlist-refresh icon-button" aria-label="刷新自选" title="刷新自选" disabled={!ready || saving} onClick={() => setWatchRefresh(n => n + 1)}><RefreshCw size={15} aria-hidden="true" /></button>}
            </div>
            <div className="results-caption">{listMode === 'watchlist' ? '关注的证券，随时继续研究' : searchedKey ? `“${searchedKey}” · ${results.length} 个结果` : '在上方输入名称或代码'}</div>
            {listMode === 'search' && searchError && <div className="error" role="alert">{searchError}</div>}
            {listMode === 'search' && searching ? <div className="empty-state" role="status">正在搜索证券…</div>
              : <SecurityList items={items} selected={selected} onSelect={onSelect} emptyText={listMode === 'watchlist' ? '暂无自选证券。搜索并选中证券后，点击“加入自选”。' : searchedKey ? '未找到证券，请尝试其他名称或代码' : '输入关键字搜索证券'} />}
          </section>
          <section className="research-workspace" aria-label="个股研究">
            {selected ? <>
              <header className="security-header"><div><h2>{selected.name}<span>{selected.ex_code}</span></h2></div><button className={watched ? 'watch-button is-watched' : 'watch-button'} aria-pressed={watched} aria-label={watched ? `将${selected.name}移出自选` : `将${selected.name}加入自选`} title={watched ? '点击移出自选' : '点击加入自选'} disabled={!ready || saving} onClick={toggleWatch}><Star size={15} fill={watched ? 'currentColor' : 'none'} aria-hidden="true" />{saving ? '保存中…' : watched ? '已加入自选' : '加入自选'}</button></header>
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
        {page === 'bond' && <Bond />}
        {page === 'metal' && <Metal />}
        {page === 'ipo' && <IPOCalendar onSelect={openIPO} />}
        {page === 'announcements' && <div className="global-announcements page-panel"><div className="scope-bar"><span>公告范围</span><button aria-pressed={announcementScope === 'latest' || !aShare} onClick={() => setAnnouncementScope('latest')}>最新披露</button><button disabled={!aShare} aria-pressed={announcementScope === 'selected' && !!aShare} onClick={() => setAnnouncementScope('selected')}>{selected ? selected.name : '当前证券'}</button></div><Announcements compact key={announcementScope === 'selected' && aShare ? selected!.ex_code : 'latest'} code={announcementScope === 'selected' && aShare ? selected!.ex_code : ''} /></div>}
        {page === 'settings' && (ready ? <Settings settings={workspace.settings} saving={saving} onSave={saveSettings} /> : <div className="page-panel empty-state">读取本地数据后可修改设置</div>)}
        </div>
      </main>
    </div>
  </div>
}
