import { useEffect, useState } from 'react'
import { company, dividends } from '../api'
import CompanyMetrics from './CompanyMetrics'
import type { BasicSecurity, Company, Dividend } from '../types'

interface Props {
  security: BasicSecurity
  tab: 'company' | 'dividends'
}

export default function SecurityDetails({ security, tab }: Props) {
  const [corp, setCorp] = useState<Company | null>(null)
  const [records, setRecords] = useState<Dividend[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  const supported = security.security_type === 'stock' && /^(SH|SZ|BJ)\d{6}$/i.test(security.ex_code)

  useEffect(() => {
    if (!supported) return
    const controller = new AbortController()
    setLoading(true)
    setError('')
    async function load() {
      try {
        if (tab === 'company') {
          const data = await company(security.ex_code, controller.signal)
          if (!controller.signal.aborted) setCorp(data)
        } else {
          const data = await dividends(security.ex_code, controller.signal)
          if (!controller.signal.aborted) setRecords(data)
        }
      } catch (e) {
        if (!controller.signal.aborted) setError(e instanceof Error ? e.message : String(e))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }
    void load()
    return () => controller.abort()
  }, [security.ex_code, tab, supported, retry])

  if (!supported) return <div className="quote empty">公司资料与分红记录暂仅支持沪深北 A 股</div>
  if (tab === 'company') {
    const fields: [string, string][] = corp ? [
      ['英文名称', corp.en_name], ['历史简称', corp.history_name],
      ['上市日期', corp.listing_date], ['发行价格', corp.listing_price ? `${corp.listing_price} 元` : ''],
      ['公司网址', corp.website], ['注册地址', corp.register_address],
      ['办公地址', corp.business_address], ['主营业务', corp.main_business],
    ] : []
    return <section className="quote">
      <CompanyMetrics security={security} />
      {loading ? <p className="data-note" role="status">正在加载公司资料…</p>
        : error ? <div><p className="error" role="alert">{error}</p><button onClick={() => setRetry(n => n + 1)}>重新加载资料</button></div>
        : corp && <><h2>{corp.name}</h2><dl className="company-fields">
          {fields.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value?.trim() || '暂无数据'}</dd></div>)}
        </dl></>}
    </section>
  }
  if (loading) return <div className="quote empty" role="status">正在加载分红记录…</div>
  if (error) return <div className="quote"><p className="error" role="alert">{error}</p><button onClick={() => setRetry(n => n + 1)}>重新加载</button></div>

  return <section className="quote"><h2>{security.name} · 分红送转</h2>
    <p className="data-note">送股、转增及现金红利均按每 10 股计算；现金红利单位为元（税前）。</p>
    {records.length === 0 ? <p className="data-note">暂无分红记录</p> : <div className="table-scroll"><table className="dividend-table">
      <thead><tr>{['公告日期', '股权登记日', '除权除息日', '送股（股）', '转增（股）', '派息（元）'].map(label => <th key={label}>{label}</th>)}</tr></thead>
      <tbody>{records.map((d, i) => <tr key={`${d.public_date}-${i}`}>
        <td>{d.public_date || '—'}</td><td>{d.record_date || '—'}</td><td>{d.dividend_date || '—'}</td>
        <td>{d.shares}</td><td>{d.add_shares}</td><td>{d.bonus}</td>
      </tr>)}</tbody>
    </table></div>}
  </section>
}
