import { useCallback, useState } from 'react'
import { filings } from '../api'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'
import Pagination from './Pagination'

const categories = { '': '主要申报', periodic: '年报和季报', current: '重大事项', insider: '内部人交易', all: '全部' }
type Category = keyof typeof categories

// Filings 是美股公司向 SEC 提交的申报，数据来自 SEC EDGAR 官方接口。
export default function Filings({ code }: { code: string }) {
  const [page, setPage] = useState(1)
  const [category, setCategory] = useState<Category>('')
  const [copied, setCopied] = useState('')
  const [copyError, setCopyError] = useState('')
  const load = useCallback((signal: AbortSignal) => filings(code, category, page, signal), [code, category, page])
  const resource = useResource(load)
  const data = resource.data
  const copy = async (url: string) => {
    try { await navigator.clipboard.writeText(url); setCopied(url); setCopyError('') }
    catch { setCopyError('复制失败，请使用原文链接打开或复制链接地址。') }
  }
  const fiscal = data?.company.fiscal_year_end?.length === 4 ? `，财年截至 ${data.company.fiscal_year_end.slice(0, 2)}-${data.company.fiscal_year_end.slice(2)}` : ''
  return <section className="research-section">
    <div className="section-toolbar"><div className="segmented" role="group" aria-label="申报类别">{(Object.keys(categories) as Category[]).map(item => <button key={item} type="button" aria-pressed={category === item} onClick={() => { setCategory(item); setPage(1) }}>{categories[item]}</button>)}</div></div>
    {data && <p className="data-note">{data.company.name}（{data.ticker}，CIK {data.company.cik}）· {data.company.exchanges?.join('/')} · {data.company.industry}{fiscal}</p>}
    <p className="data-note">SEC EDGAR 官方申报 · {categories[category]} · 主要申报包括 10-K 年报、10-Q 季报、8-K 重大事项、20-F/6-K 外国公司年报和临时报告、委托书和招股书。联系邮箱可在设置中修改。</p>
    {data?.truncated && <p className="data-note">这家公司申报很多，只查了最近一段时间，更早的申报没有列出；可以用命令行 sec filings -b 指定开始日期查全。</p>}
    {copyError && <p className="error" role="alert">{copyError}</p>}
    <ResourceState {...resource} retry={resource.reload} />
    {data && (data.results.length === 0 ? <div className="empty-state">暂无符合条件的申报</div> : <ul className="announcement-list">{data.results.map(item => <li key={item.accession}>
      <div className="announcement-meta"><time>{item.filing_date}</time><span>{item.form}{item.report_date ? ` · 报告期 ${item.report_date}` : ''}</span></div>
      <h3>{item.description || item.form}</h3>
      <div className="announcement-actions"><a href={item.url} target="_blank" rel="noopener noreferrer" onClick={e => {
        if (!window.secOpenDisclosure) return
        e.preventDefault()
        void window.secOpenDisclosure(item.url).catch(() => setCopyError('打开原文失败，请复制链接到浏览器。'))
      }}>查看原文 ↗</a><button onClick={() => void copy(item.url)}>{copied === item.url ? '已复制链接' : '复制链接'}</button></div>
    </li>)}</ul>)}
    {data && <Pagination page={page} total={data.total} hasMore={data.has_more} loading={resource.loading} onPage={setPage} />}
  </section>
}
