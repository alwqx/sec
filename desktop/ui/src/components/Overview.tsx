import { useEffect, useState } from 'react'
import { quote } from '../api'
import type { BasicSecurity, SecurityQuote } from '../types'
import QuoteCard from './QuoteCard'
import PriceHistory from './PriceHistory'
import CompanyMetrics from './CompanyMetrics'

export default function Overview({ security, interval }: { security: BasicSecurity; interval: number }) {
  const [data, setData] = useState<SecurityQuote | null>(null)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  const fund = /^OF\d{6}$/i.test(security.ex_code)

  useEffect(() => {
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    const load = async () => {
      try {
        const value = await quote(security.ex_code, controller.signal)
        if (!controller.signal.aborted) { setData(value); setError('') }
      } catch (e) {
        if (!controller.signal.aborted) setError(e instanceof Error ? e.message : String(e))
      } finally {
        if (!controller.signal.aborted) timer = setTimeout(load, interval * 1000)
      }
    }
    void load()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [security.ex_code, interval, retry])

  return <div className="overview">
    <div className="section-caption"><span>实时行情</span><span>每 {interval} 秒刷新 · 红涨绿跌</span></div>
    {error && <div className="error" role="alert">{error}<button onClick={() => setRetry(n => n + 1)}>重试</button></div>}
    {data ? <QuoteCard quote={data} /> : !error && <div className="empty-state" role="status">正在获取行情…</div>}
    <CompanyMetrics security={security} refreshSeconds={60} />
    {/* 历史走势接口还不支持 OF 开头的基金代码，先不显示，免得只看到报错。 */}
    {fund ? <p className="data-note">基金的历史走势暂不支持。</p> : <PriceHistory code={security.ex_code} />}
    <p className="data-note">行情以数据源返回时间为准，港美股可能存在延迟。</p>
  </div>
}
