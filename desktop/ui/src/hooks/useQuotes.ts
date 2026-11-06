import { useEffect, useMemo, useState } from 'react'
import { quotes as fetchQuotes } from '../api'
import type { SecurityQuote } from '../types'

// 昨收缺失时无法计算涨跌，不显示为 0。
// priceDigits 是价格的小数位数：基金净值 4 位，场内基金 3 位（交易所报价精确到 0.001），其他 2 位。
export function priceDigits(quote: SecurityQuote) {
  if (quote.nav) return 4
  return /^OF\d{6}$/i.test(quote.ex_code) ? 3 : 2
}

export function quoteChange(quote: SecurityQuote) {
  const known = quote.yclose > 0 && quote.current > 0
  const change = quote.current - quote.yclose
  return {
    direction: !known || change === 0 ? 'flat' : change > 0 ? 'up' : 'down',
    changeText: known ? `${change > 0 ? '+' : ''}${change.toFixed(priceDigits(quote))}` : '—',
    percentText: known ? `${change > 0 ? '+' : ''}${(change / quote.yclose * 100).toFixed(2)}%` : '—',
  }
}

export interface QuotesState { quotes: Map<string, SecurityQuote>; missing: string[]; error: string; updatedAt: Date | null; reload: () => void }

// 刷新失败时保留上一次的行情并显示错误，不清空列表。
export function useQuotes(codes: string[], intervalSeconds: number, enabled: boolean): QuotesState {
  const [data, setData] = useState<{ quotes: Map<string, SecurityQuote>; missing: string[]; updatedAt: Date | null }>({ quotes: new Map(), missing: [], updatedAt: null })
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  const key = codes.join(',')
  useEffect(() => {
    if (!enabled || !key) return
    const controller = new AbortController()
    // 上一次请求没返回时跳过这一轮，请求不堆积，旧响应也不会覆盖新数据。
    let busy = false
    const refresh = () => {
      if (busy) return
      busy = true
      void fetchQuotes(key.split(','), controller.signal).then(result => {
        setData({ quotes: new Map(result.results.map(item => [item.ex_code.toUpperCase(), item])), missing: result.missing, updatedAt: new Date() })
        setError('')
      }).catch((e: unknown) => {
        if (!controller.signal.aborted) setError(e instanceof Error ? e.message : String(e))
      }).finally(() => { busy = false })
    }
    refresh()
    const timer = window.setInterval(refresh, Math.max(1, intervalSeconds) * 1000)
    return () => { controller.abort(); window.clearInterval(timer) }
  }, [key, intervalSeconds, enabled, retry])
  return useMemo(() => ({ ...data, error, reload: () => setRetry(value => value + 1) }), [data, error])
}
