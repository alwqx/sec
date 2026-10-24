import { useCallback, useState, type FormEvent } from 'react'
import { bond } from '../api'
import { useResource } from '../hooks/useResource'
import type { BondData } from '../types'
import BondChart from './BondChart'
import ResourceState from './ResourceState'
import Pagination from './Pagination'

function todayNewYork() {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date())
  const value = (key: string) => parts.find(part => part.type === key)!.value
  return `${value('year')}-${value('month')}-${value('day')}`
}
function dateRange(days: number) {
  const end = todayNewYork(), date = new Date(`${end}T00:00:00Z`)
  date.setUTCDate(date.getUTCDate() - days)
  return { begin: date.toISOString().slice(0, 10), end }
}
function yieldText(value: number | null | undefined) { return value == null ? '—' : `${value.toFixed(2)}%` }
function bpText(value: number | null) { return value === null ? '—' : `${value > 0 ? '+' : ''}${value.toFixed(1)} bp` }
function changeClass(value: number | null) { return value === null || value === 0 ? 'flat' : value > 0 ? 'up' : 'down' }

function BondResults({ data, tenor, setTenor }: { data: BondData; tenor: string; setTenor: (tenor: string) => void }) {
  const [page, setPage] = useState(1)
  const [curveDate, setCurveDate] = useState('latest')
  const latest = data.latest
  const selectedDay = curveDate === 'latest' ? latest : data.results.find(item => item.date === curveDate) ?? latest
  const label = data.tenors.find(item => item.key === tenor)?.label ?? '10年'
  const pageSize = 30, rows = [...data.results].reverse().slice((page - 1) * pageSize, page * pageSize)
  return <>
    <section className="page-panel bond-latest" aria-label="最新美债收益率">
      <div className="section-caption"><h2>美国国债收益率</h2><span>{latest ? `最新交易日 ${latest.date}` : '近期暂无数据'}</span></div>
      {latest ? <>
        <div className="bond-benchmark"><div><span className="data-note">10 年期收益率</span><div className="quote-price">{yieldText(latest.yields['10y'])}</div><div className={`quote-change ${changeClass(latest.change_bp)}`}>较前一交易日 {bpText(latest.change_bp)}</div></div><div className="data-note">前值 {yieldText(latest.previous_10y)}<br />1 bp = 0.01 个百分点</div></div>
        <dl className="quote-grid bond-yields">{data.tenors.map(item => <div key={item.key}><dt>{item.label}</dt><dd>{yieldText(latest.yields[item.key])}</dd></div>)}</dl>
      </> : <div className="empty-state">暂无最近可用的美债日收益率</div>}
      <p className="data-note">来源：美国财政部 · 每日公布，非盘中实时报价；红绿表示收益率上升或下降，不表示债券价格涨跌。缺失值显示“—”。</p>
    </section>
    <section className="page-panel" aria-label="美债收益率曲线">
      <div className="section-toolbar"><h2>收益率曲线</h2><label>对比日期<select aria-label="收益率曲线日期" value={curveDate} onChange={e => setCurveDate(e.target.value)}><option value="latest">最新交易日</option>{[...data.results].reverse().map(item => <option key={item.date} value={item.date}>{item.date}</option>)}</select></label></div>
      {selectedDay ? <BondChart key={selectedDay.date} title={`${selectedDay.date} · 各期限收益率`} curve points={data.tenors.map(item => ({ label: item.label, value: selectedDay.yields[item.key] ?? null }))} /> : <div className="empty-state">暂无收益率曲线</div>}
    </section>
    <section className="page-panel" aria-label="美债历史收益率">
      <div className="section-toolbar"><h2>历史走势</h2><label>期限<select aria-label="历史收益率期限" value={tenor} onChange={e => setTenor(e.target.value)}>{data.tenors.map(item => <option key={item.key} value={item.key}>{item.label}</option>)}</select></label></div>
      <p className="data-note">{data.begin} 至 {data.end} · {data.results.length} 个交易日 · 收益率单位 %，10 年期日变动单位 bp</p>
      {data.results.length ? <>
        <BondChart key={tenor} title={`${label}期 · 历史收益率`} points={data.results.map(item => ({ label: item.date, value: item.yields[tenor] ?? null }))} />
        <div className="table-scroll"><table className="data-table bond-table"><thead><tr><th>日期</th>{data.tenors.map(item => <th key={item.key}>{item.label}</th>)}<th>10年期前值</th><th>10年期变动</th></tr></thead><tbody>{rows.map(item => <tr key={item.date}><th scope="row">{item.date}</th>{data.tenors.map(tenor => <td key={tenor.key}>{yieldText(item.yields[tenor.key])}</td>)}<td>{yieldText(item.previous_10y)}</td><td className={changeClass(item.change_bp)}>{bpText(item.change_bp)}</td></tr>)}</tbody></table></div>
        <Pagination page={page} total={data.results.length} hasMore={page * pageSize < data.results.length} loading={false} onPage={setPage} />
      </> : <div className="empty-state">所选区间没有交易记录，请调整日期。</div>}
    </section>
  </>
}

export default function Bond() {
  const [tenor, setTenor] = useState('10y')
  const [range, setRange] = useState(() => dateRange(30))
  const [draft, setDraft] = useState(range)
  const [validation, setValidation] = useState('')
  const load = useCallback((signal: AbortSignal) => bond(range.begin, range.end, signal), [range.begin, range.end])
  const resource = useResource(load)
  const apply = (next: typeof range) => {
    setValidation('')
    if (next.begin === range.begin && next.end === range.end) resource.reload()
    else setRange(next)
  }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!draft.begin || !draft.end) { setValidation('请选择开始和结束日期'); return }
    if (draft.begin > draft.end) { setValidation('开始日期不能晚于结束日期'); return }
    apply(draft)
  }
  const quickRange = (days: number) => { const next = dateRange(days); setDraft(next); apply(next) }
  return <div className="bond-page">
    <section className="page-panel" aria-label="美债收益率查询"><form className="section-toolbar bond-range-form" onSubmit={submit}>
      <label htmlFor="bond-begin">开始日期<input id="bond-begin" type="date" required min="1990-01-01" max={draft.end} value={draft.begin} onChange={e => setDraft({ ...draft, begin: e.target.value })} /></label>
      <label htmlFor="bond-end">结束日期<input id="bond-end" type="date" required min={draft.begin} max={todayNewYork()} value={draft.end} onChange={e => setDraft({ ...draft, end: e.target.value })} /></label>
      <button className="primary-button" type="submit">查询</button><button type="button" disabled={resource.loading} onClick={resource.reload}>刷新</button>
    </form><div className="bond-quick-ranges"><span className="data-note">快捷区间</span>{[[30, '近1个月'], [90, '近3个月'], [365, '近1年']].map(([days, label]) => <button key={days} onClick={() => quickRange(Number(days))}>{label}</button>)}</div>
      <p className="data-note">日期按美东时区计算，支持 1990 年至今，单次历史区间最多 5 年。修改历史区间不会改变最新行情的日期。</p>
      {validation && <div className="error" role="alert">{validation}</div>}
    </section>
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && <BondResults key={`${resource.data.begin}-${resource.data.end}`} data={resource.data} tenor={tenor} setTenor={setTenor} />}
  </div>
}
