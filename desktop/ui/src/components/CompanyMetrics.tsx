import { useEffect, useState } from 'react'
import { companyMetrics } from '../api'
import type { BasicSecurity, CompanyMetrics as Metrics } from '../types'

function ratio(value: number | null) {
  return value === null ? '—' : value.toFixed(2)
}
function capital(value: number | null) {
  if (value === null) return '—'
  if (value >= 1e12) return `${(value / 1e12).toFixed(2)} 万亿元`
  if (value >= 1e8) return `${(value / 1e8).toFixed(2)} 亿元`
  if (value >= 1e4) return `${(value / 1e4).toFixed(2)} 万元`
  return `${value.toFixed(2)} 元`
}

export default function CompanyMetrics({ security, refreshSeconds }: { security: BasicSecurity; refreshSeconds?: number }) {
  const supported = security.security_type === 'stock' && /^(SH|SZ|BJ)\d{6}$/i.test(security.ex_code)
  const [data, setData] = useState<Metrics | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    if (!supported) return
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    setData(null)
    setLoading(true)
    setError('')
    const load = async () => {
      try {
        const value = await companyMetrics(security.ex_code, controller.signal)
        if (!controller.signal.aborted) { setData(value); setError('') }
      } catch (e) {
        if (!controller.signal.aborted) setError(e instanceof Error ? e.message : String(e))
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
          if (refreshSeconds) timer = setTimeout(load, refreshSeconds * 1000)
        }
      }
    }
    void load()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [security.ex_code, supported, refreshSeconds, retry])
  if (!supported) return <p className="data-note">公司核心指标暂仅支持沪深北 A 股。</p>
  return <section className="company-metrics" aria-label="公司核心指标">
    <div className="section-caption"><h3>公司核心指标</h3><button onClick={() => setRetry(n => n + 1)} disabled={loading}>刷新指标</button></div>
    {loading && <p className="data-note" role="status">正在加载公司核心指标…</p>}
    {error && <p className="error" role="alert">{error}{data && '（以下为上次成功获取的快照）'}<button onClick={() => setRetry(n => n + 1)}>重试</button></p>}
    {data && <>
      <dl className="quote-grid metrics-grid">
        <div><dt>行业</dt><dd>{data.industry || '—'}</dd></div>
        <div><dt>市盈率 PE（TTM）</dt><dd>{ratio(data.pe_ttm)}</dd></div>
        <div><dt>市净率 PB</dt><dd>{ratio(data.pb)}</dd></div>
        <div><dt>总市值</dt><dd>{capital(data.market_cap)}</dd></div>
        <div><dt>流通市值</dt><dd>{capital(data.float_market_cap)}</dd></div>
      </dl>
      <p className="data-note">指标快照：{data.trade_date || '日期未知'} {data.time} · 市值币种：人民币{refreshSeconds ? ` · 每 ${refreshSeconds} 秒刷新` : ''}<br />计算口径与 CLI info 一致；缺失值、不适用的 PE 及无法计算的指标显示“—”。</p>
    </>}
  </section>
}
