import { useCallback, useState, type FormEvent } from 'react'
import { commodities, commodityHistory } from '../api'
import { useResource } from '../hooks/useResource'
import { marketToday, monthsAgo, rangeLabel } from '../marketDate'
import type { CommodityQuote } from '../types'
import CandlestickChart from './CandlestickChart'
import PageActions from './PageActions'
import Pagination from './Pagination'
import ResourceState from './ResourceState'

const direction = (value: number | null) => value === null || value === 0 ? 'flat' : value > 0 ? 'up' : 'down'
const signed = (value: number | null, percent = false) => value === null ? '—' : `${value > 0 ? '+' : ''}${value.toFixed(2)}${percent ? '%' : ''}`
const price = (value: number | null) => value === null ? '—' : String(Number(value.toFixed(3)))

function Overview({ quotes, selected, onSelect }: { quotes: CommodityQuote[]; selected: string; onSelect: (id: string) => void }) {
  return <div className="table-scroll"><table className="data-table commodity-table">
    <thead><tr>{['品种', '最新', '涨跌', '涨跌幅', '开盘', '最高', '最低', '昨结算', '单位', '时间'].map(label => <th key={label}>{label}</th>)}</tr></thead>
    <tbody>{quotes.map(q => <tr key={q.id} aria-selected={selected === q.id}>
      <th scope="row"><button type="button" className="link-button" onClick={() => onSelect(q.id)}>{q.name}</button><small>{q.domestic ? '国内' : '国际'} · {q.exchange}</small></th>
      <td className={direction(q.change)}>{price(q.price)}</td><td className={direction(q.change)}>{signed(q.change)}</td><td className={direction(q.change)}>{signed(q.change_rate, true)}</td>
      <td>{price(q.open)}</td><td>{price(q.high)}</td><td>{price(q.low)}</td><td>{price(q.prev_settle)}</td><td>{q.unit}</td><td>{q.date} {q.time}</td>
    </tr>)}</tbody>
  </table></div>
}

function History({ id, name }: { id: string; name: string }) {
  const [range, setRange] = useState(() => ({ begin: monthsAgo(3), end: marketToday() }))
  const [draft, setDraft] = useState(range)
  const [validation, setValidation] = useState('')
  const [page, setPage] = useState(1)
  const load = useCallback((signal: AbortSignal) => commodityHistory(id, range.begin, range.end, signal), [id, range.begin, range.end])
  const resource = useResource(load)
  const apply = (next: typeof range) => { setValidation(''); setPage(1); setDraft(next); setRange(next) }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!draft.begin || !draft.end) { setValidation('请选择开始和结束日期'); return }
    if (draft.begin > draft.end) { setValidation('开始日期不能晚于结束日期'); return }
    apply(draft)
  }
  const data = resource.data
  const pageSize = 30
  const currentPage = Math.min(page, Math.max(1, Math.ceil((data?.results.length ?? 0) / pageSize)))
  const rows = data ? [...data.results].reverse().slice((currentPage - 1) * pageSize, currentPage * pageSize) : []
  const unit = data?.commodity.unit ?? ''
  // 国内品种前一天没有结算价时，涨跌相对收盘价，标 *。
  const closeBasis = (basis: string) => data?.commodity.domestic && basis === 'close' 
  return <section className="page-panel metal-history" aria-label={`${name} 历史行情`}>
    <div className="section-caption"><h2>{name} 日行情</h2>{data && <span>{data.commodity.exchange} · {data.begin} 至 {data.end} · {data.results.length} 个交易日</span>}</div>
    <form className="section-toolbar metal-range-form" onSubmit={submit}>
      <label htmlFor="commodity-begin">开始日期<input id="commodity-begin" type="date" required value={draft.begin} onChange={e => setDraft({ ...draft, begin: e.target.value })} /></label>
      <label htmlFor="commodity-end">结束日期<input id="commodity-end" type="date" required value={draft.end} onChange={e => setDraft({ ...draft, end: e.target.value })} /></label>
      <button className="primary-button" type="submit">查询</button>
    </form>
    <div className="metal-quick-ranges"><span className="data-note">快捷区间</span>{[3, 12, 36].map(months => <button type="button" key={months} onClick={() => apply({ begin: monthsAgo(months), end: marketToday() })}>{rangeLabel(months)}</button>)}</div>
    {validation && <p className="error" role="alert">{validation}</p>}
    <ResourceState {...resource} retry={resource.reload} />
    {data && (data.results.length === 0 ? <div className="empty-state">所选区间暂无交易记录，请调整日期范围。</div> : <>
      <CandlestickChart key={`${id}-${data.begin}-${data.end}`} data={data.results} name={name} unit={unit} />
      <div className="section-caption metal-table-heading"><span>日行情明细 · 最新在前</span><span>价格单位：{unit}</span></div>
      <div className="table-scroll"><table className="data-table metal-table"><thead><tr>{['日期', '收盘', '涨跌', '涨跌幅', '开盘', '最高', '最低', data.commodity.domestic ? '结算' : null, '成交量'].filter(Boolean).map(label => <th key={label}>{label}</th>)}</tr></thead>
        <tbody>{rows.map(bar => <tr key={bar.date}>
          <th scope="row">{bar.date}</th><td className={direction(bar.change)}>{price(bar.close)}</td><td className={direction(bar.change)} title={closeBasis(bar.basis) ? '前一日没有结算价，相对收盘价' : undefined}>{signed(bar.change)}{closeBasis(bar.basis) && '*'}</td><td className={direction(bar.change)}>{signed(bar.change_rate, true)}</td>
          <td>{price(bar.open)}</td><td>{price(bar.high)}</td><td>{price(bar.low)}</td>{data.commodity.domestic && <td>{price(bar.settle)}</td>}<td>{bar.volume ? bar.volume.toLocaleString('zh-CN') : '—'}</td>
        </tr>)}</tbody></table></div>
      <Pagination page={currentPage} total={data.results.length} hasMore={currentPage * pageSize < data.results.length} loading={resource.loading} onPage={setPage} />
      <p className="data-note">{data.commodity.domestic ? '主力连续合约，换月时价格会跳空，适合看走势，不适合计算长期收益；涨跌相对前一日结算价，前一日没有结算价（早年数据常见）时相对收盘价并标 *；成交量单位为手。' : '新浪外盘日行情没有结算价，涨跌相对前一日收盘价，和上方表格（相对昨日结算价）略有差别；历史行情大多没有成交量。'}</p>
    </>)}
  </section>
}

export default function Commodity() {
  const [selected, setSelected] = useState('sc')
  const load = useCallback((signal: AbortSignal) => commodities(signal), [])
  const resource = useResource(load)
  const data = resource.data
  const name = data?.results.find(q => q.id === selected)?.name ?? selected
  return <div className="metal-page">
    <section className="page-panel" aria-label="大宗商品行情">
      <div className="section-caption"><h2>大宗商品</h2><span>点击品种查看日 K 线</span></div>
      <PageActions><button type="button" onClick={resource.reload} disabled={resource.loading}>刷新行情</button></PageActions>
      <ResourceState {...resource} retry={resource.reload} />
      {data && <Overview quotes={data.results} selected={selected} onSelect={setSelected} />}
      {data?.failed?.length ? <p className="error" role="alert">未取到：{data.failed.join('；')}</p> : null}
      <p className="data-note">国内为主力连续合约，国际为纽约、伦敦期货；涨跌相对昨日结算价。行情来自新浪期货，有延迟，不构成投资建议。</p>
    </section>
    <History key={selected} id={selected} name={name} />
  </div>
}
