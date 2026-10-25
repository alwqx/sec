import { useCallback, useMemo, useState, type FormEvent } from 'react'
import { history } from '../api'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'
import CandlestickChart from './CandlestickChart'

const adjustments = { bfq: '不复权', qfq: '前复权', hfq: '后复权' }
type Adjustment = keyof typeof adjustments

function marketToday() {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date())
  const value = (name: string) => parts.find(part => part.type === name)!.value
  return `${value('year')}-${value('month')}-${value('day')}`
}

// 按自然月回溯，月末日期限制为目标月份的最后一天。
function recentRange(months: number) {
  const end = marketToday()
  const date = new Date(`${end}T00:00:00Z`)
  const day = date.getUTCDate()
  date.setUTCDate(1)
  date.setUTCMonth(date.getUTCMonth() - months)
  const lastDay = new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth() + 1, 0)).getUTCDate()
  date.setUTCDate(Math.min(day, lastDay))
  return { begin: date.toISOString().slice(0, 10), end, fq: 'bfq' as Adjustment }
}

function LoadedHistory({ code }: { code: string }) {
  const [filters, setFilters] = useState(() => recentRange(3))
  const [draft, setDraft] = useState(filters)
  const [validation, setValidation] = useState('')
  const load = useCallback((signal: AbortSignal) => history(code, filters, signal), [code, filters])
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
  const values = useMemo(() => resource.data?.filter(item =>
    [item.open, item.close, item.high, item.low].every(Number.isFinite),
  ) ?? [], [resource.data])
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
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && (values.length ? <CandlestickChart key={`${code}-${filters.begin}-${filters.end}-${filters.fq}`} data={values} name={code} showVolume unit={code.startsWith('HK') ? '港元' : '元'} /> : <p className="data-note">所选区间暂无历史行情</p>)}
  </div>
}

export default function PriceHistory({ code }: { code: string }) {
  return /^(SH|SZ)\d{6}$|^HK\d{5}$/i.test(code) ? <LoadedHistory key={code} code={code} /> : <p className="data-note">走势图暂支持沪深证券与港股。</p>
}
