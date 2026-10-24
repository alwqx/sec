import { useCallback, useState, type FormEvent } from 'react'
import { metal } from '../api'
import { useResource } from '../hooks/useResource'
import type { MetalQuote } from '../types'
import CandlestickChart from './CandlestickChart'
import ResourceState from './ResourceState'
import Pagination from './Pagination'

// 交易日与默认查询日期统一使用上海时区，避免本机位于海外时跨日偏移。
function shanghaiDate() {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date())
  const value = (name: string) => parts.find(part => part.type === name)!.value
  return `${value('year')}-${value('month')}-${value('day')}`
}

function dateRange(days: number) {
  const end = shanghaiDate()
  const begin = new Date(`${end}T00:00:00Z`)
  begin.setUTCDate(begin.getUTCDate() - days)
  return { begin: begin.toISOString().slice(0, 10), end }
}

function direction(value: number | null) {
  return value === null || value === 0 ? 'flat' : value > 0 ? 'up' : 'down'
}

function change(value: number | null, percent = false) {
  return value === null ? '—' : `${value > 0 ? '+' : ''}${value.toFixed(2)}${percent ? '%' : ''}`
}

function Latest({ quote, name, unit }: { quote: MetalQuote; name: string; unit: string }) {
  return <section className="metal-latest" aria-label="最新黄金日行情">
    <div className="metal-quote-heading"><div><span className="eyebrow">上海黄金交易所</span><h2>{name}<span>黄金现货</span></h2></div><span className="data-note">最新交易日 {quote.date}</span></div>
    <div className="metal-price-row"><div><span className="data-note">收盘价 · {unit}</span><div className={`quote-price ${direction(quote.change)}`}>{quote.close.toFixed(2)}</div><div className={`quote-change ${direction(quote.change)}`}>{change(quote.change)} {unit}（{change(quote.change_rate, true)}）</div></div>
      <dl className="quote-grid metal-quote-grid">{[['开盘', quote.open], ['最高', quote.high], ['最低', quote.low], ['昨收', quote.yclose]].map(([label, value]) => <div key={String(label)}><dt>{label}</dt><dd>{typeof value === 'number' ? value.toFixed(2) : '—'}</dd></div>)}</dl>
    </div>
    <p className="data-note">日行情，非盘中实时报价；休市时展示最近可用交易日。涨跌相对上一条可用日行情的收盘价计算，缺少昨收时显示“—”。</p>
  </section>
}

export default function Metal() {
  const [range, setRange] = useState(() => dateRange(30))
  const [draft, setDraft] = useState(range)
  const [validation, setValidation] = useState('')
  const [page, setPage] = useState(1)
  const load = useCallback((signal: AbortSignal) => metal(range.begin, range.end, signal), [range.begin, range.end])
  const resource = useResource(load)
  const apply = (next: typeof range) => {
    setValidation('')
    setPage(1)
    setRange(next)
    if (next.begin === range.begin && next.end === range.end) resource.reload()
  }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!draft.begin || !draft.end) { setValidation('请选择开始和结束日期'); return }
    if (draft.begin > draft.end) { setValidation('开始日期不能晚于结束日期'); return }
    apply(draft)
  }
  const quickRange = (days: number) => { const next = dateRange(days); setDraft(next); apply(next) }
  const data = resource.data
  const pageSize = 30
  const currentPage = Math.min(page, Math.max(1, Math.ceil((data?.results.length ?? 0) / pageSize)))
  const rows = data ? [...data.results].reverse().slice((currentPage - 1) * pageSize, currentPage * pageSize) : []

  return <div className="metal-page">
    <section className="page-panel metal-controls" aria-label="黄金历史查询">
      <form className="section-toolbar metal-range-form" onSubmit={submit}>
        <label htmlFor="metal-begin">开始日期<input id="metal-begin" type="date" required value={draft.begin} onChange={e => setDraft({ ...draft, begin: e.target.value })} /></label>
        <label htmlFor="metal-end">结束日期<input id="metal-end" type="date" required value={draft.end} onChange={e => setDraft({ ...draft, end: e.target.value })} /></label>
        <button className="primary-button" type="submit">查询</button>
        <button type="button" onClick={resource.reload} disabled={resource.loading}>刷新</button>
      </form>
      <div className="metal-quick-ranges"><span className="data-note">快捷区间</span>{[[30, '近 1 个月'], [90, '近 3 个月'], [365, '近 1 年']].map(([days, label]) => <button type="button" key={days} onClick={() => quickRange(Number(days))}>{label}</button>)}</div>
      {validation && <p className="error" role="alert">{validation}</p>}
    </section>
    <ResourceState {...resource} retry={resource.reload} />
    {data && <>
      <div className="page-panel">{data.latest ? <Latest quote={data.latest} name={data.name} unit={data.unit} /> : <div className="empty-state">暂无黄金日行情</div>}</div>
      <section className="page-panel metal-history" aria-label="黄金历史行情">
        <div className="section-caption"><h2>历史行情</h2><span>{data.begin} 至 {data.end} · {data.results.length} 个交易日</span></div>
        {data.results.length === 0 ? <div className="empty-state">所选区间暂无交易记录，请调整日期范围。</div> : <>
          <CandlestickChart key={`${data.begin}-${data.end}`} data={data.results} name={data.name} unit={data.unit} />
          <div className="section-caption metal-table-heading"><span>日行情明细 · 最新在前</span><span>价格单位：{data.unit}</span></div>
          <div className="table-scroll"><table className="data-table metal-table"><thead><tr>{['日期', '收盘', '涨跌', '涨跌幅', '开盘', '最高', '最低', '昨收'].map(label => <th key={label}>{label}</th>)}</tr></thead><tbody>{rows.map(item => <tr key={item.date}>
            <th scope="row">{item.date}</th><td className={direction(item.change)}>{item.close.toFixed(2)}</td><td className={direction(item.change)}>{change(item.change)}</td><td className={direction(item.change)}>{change(item.change_rate, true)}</td><td>{item.open.toFixed(2)}</td><td>{item.high.toFixed(2)}</td><td>{item.low.toFixed(2)}</td><td>{item.yclose?.toFixed(2) ?? '—'}</td>
          </tr>)}</tbody></table></div>
          <Pagination page={currentPage} total={data.results.length} hasMore={currentPage * pageSize < data.results.length} loading={resource.loading} onPage={setPage} />
        </>}
      </section>
    </>}
  </div>
}
