import { useCallback, useMemo, useState, type FormEvent } from 'react'
import { history } from '../api'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'
import CandlestickChart from './CandlestickChart'
import { marketToday, monthsAgo } from '../marketDate'

const adjustments = { bfq: '不复权', qfq: '前复权', hfq: '后复权' }
const maPeriods = [5, 10, 20, 60]
type Adjustment = keyof typeof adjustments

function recentRange(months: number) {
  return { begin: monthsAgo(months), end: marketToday(), fq: 'bfq' as Adjustment }
}

function LoadedHistory({ code }: { code: string }) {
  const [filters, setFilters] = useState(() => recentRange(3))
  const [draft, setDraft] = useState(filters)
  const [validation, setValidation] = useState('')
  // 指标随行情一次取回，切换时不重新请求，图表也不会滚回最新交易日。
  const load = useCallback((signal: AbortSignal) => history(code, { ...filters, ma: maPeriods.join(','), boll: '20,2' }, signal), [code, filters])
  const [shown, setShown] = useState<string[]>([])
  const resource = useResource(load)
  const apply = (next: typeof filters) => {
    setValidation('')
    setFilters(next)
    if (next.begin === filters.begin && next.end === filters.end && next.fq === filters.fq) resource.reload()
  }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!draft.begin || !draft.end) { setValidation('请选择开始和结束日期'); return }
    if (draft.begin > draft.end) { setValidation('开始日期不能晚于结束日期'); return }
    const limit = new Date(`${draft.begin}T00:00:00Z`)
    limit.setUTCFullYear(limit.getUTCFullYear() + 5)
    if (draft.begin < '1990-01-01' || draft.end > marketToday() || draft.end > limit.toISOString().slice(0, 10)) {
      setValidation('日期范围须在 1990 年至今日之间，单次最多 5 年'); return
    }
    apply(draft)
  }
  const quickRange = (months: number) => {
    const next = { ...recentRange(months), fq: draft.fq }
    setDraft(next)
    apply(next)
  }
  // 保持历史数据引用稳定，避免实时行情刷新时触发 K 线自动滚回最新交易日。
  const results = resource.data?.results
  const keep = useMemo(() => (results ?? []).map(item => [item.open, item.close, item.high, item.low].every(Number.isFinite)), [results])
  const values = useMemo(() => (results ?? []).filter((_, index) => keep[index]), [results, keep])
  const overlays = useMemo(() => (resource.data?.overlays ?? [])
    .filter(overlay => shown.includes(overlay.kind === 'boll' ? 'BOLL' : overlay.label))
    .map(overlay => ({ ...overlay, values: overlay.values.filter((_, index) => keep[index]) })), [resource.data, shown, keep])
  const toggle = (name: string) => setShown(current => current.includes(name) ? current.filter(item => item !== name) : [...current, name])
  return <div className="history-section">
    <form className="history-filters" onSubmit={submit} aria-label="历史行情查询">
      <label>开始日期<input aria-label="历史行情开始日期" type="date" min="1990-01-01" max={marketToday()} value={draft.begin} onChange={e => setDraft({ ...draft, begin: e.target.value })} required /></label>
      <label>结束日期<input aria-label="历史行情结束日期" type="date" min="1990-01-01" max={marketToday()} value={draft.end} onChange={e => setDraft({ ...draft, end: e.target.value })} required /></label>
      <label>复权方式<select aria-label="历史行情复权方式" value={draft.fq} onChange={e => setDraft({ ...draft, fq: e.target.value as Adjustment })}>{Object.entries(adjustments).map(([value, title]) => <option key={value} value={value}>{title}</option>)}</select></label>
      <button type="submit">查询</button>
    </form>
    <div className="history-shortcuts" aria-label="历史行情快捷区间">{[[1, '近 1 个月'], [3, '近 3 个月'], [6, '近 6 个月'], [12, '近 1 年']].map(([months, label]) => <button key={months} onClick={() => quickRange(Number(months))}>{label}</button>)}</div>
    {validation && <p className="error" role="alert">{validation}</p>}
    <p className="data-note">{filters.begin} 至 {filters.end} · {adjustments[filters.fq]} · 日 K 线；休市日无记录。前复权／后复权仅影响历史行情，实时行情仍为实际报价。</p>
    <div className="history-indicators" role="group" aria-label="技术指标"><span className="data-note">指标</span>{[...maPeriods.map(period => `MA${period}`), 'BOLL'].map(name => <label key={name}><input type="checkbox" checked={shown.includes(name)} onChange={() => toggle(name)} />{name === 'BOLL' ? 'BOLL(20,2)' : name}</label>)}</div>
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data?.source_note && <p className="data-note">{resource.data.source_note}</p>}
    {resource.data && (values.length ? <CandlestickChart key={`${code}-${filters.begin}-${filters.end}-${filters.fq}`} data={values} overlays={overlays} name={code} showVolume unit={code.startsWith('HK') ? '港元' : '元'} /> : <p className="data-note">所选区间暂无历史行情</p>)}
  </div>
}

export default function PriceHistory({ code }: { code: string }) {
  return /^(SH|SZ)\d{6}$|^HK\d{5}$/i.test(code) ? <LoadedHistory key={code} code={code} /> : <p className="data-note">走势图暂支持沪深证券与港股。</p>
}
