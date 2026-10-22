import { useCallback, useEffect, useRef, useState } from 'react'
import { quote, search } from './api'
import type { BasicSecurity, SecurityQuote } from './types'
import SearchBar from './components/SearchBar'
import SecurityList from './components/SecurityList'
import QuoteCard from './components/QuoteCard'

export default function App() {
  const [results, setResults] = useState<BasicSecurity[]>([])
  const [selected, setSelected] = useState<BasicSecurity | null>(null)
  const [quoteData, setQuoteData] = useState<SecurityQuote | null>(null)
  const [searching, setSearching] = useState(false)
  const [error, setError] = useState('')
  const quoteController = useRef<AbortController | null>(null)

  // 每轮请求完成后再等待 5 秒，避免慢请求重叠；清理时丢弃旧响应。
  useEffect(() => {
    if (!selected) return
    const controller = new AbortController()
    quoteController.current = controller
    let timer: ReturnType<typeof setTimeout> | undefined

    const loadQuote = async () => {
      if (controller.signal.aborted) return
      try {
        const data = await quote(selected.ex_code, controller.signal)
        if (controller.signal.aborted) return
        setQuoteData(data)
        setError('')
      } catch (e) {
        if (controller.signal.aborted) return
        setError(e instanceof Error ? e.message : String(e))
      } finally {
        if (!controller.signal.aborted) {
          timer = setTimeout(loadQuote, 5000)
        }
      }
    }

    void loadQuote()
    return () => {
      controller.abort()
      clearTimeout(timer)
      if (quoteController.current === controller) quoteController.current = null
    }
  }, [selected])

  const onSelect = useCallback((sec: BasicSecurity) => {
    // 在 effect 清理执行前同步失效旧请求，防止其在切换时写入状态。
    if (sec === selected) return
    quoteController.current?.abort()
    setQuoteData(null)
    setError('')
    setSelected(sec)
  }, [selected])

  const onSearch = useCallback(async (key: string) => {
    quoteController.current?.abort()
    setSearching(true)
    setError('')
    setSelected(null)
    setQuoteData(null)
    try {
      setResults(await search(key))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setResults([])
    } finally {
      setSearching(false)
    }
  }, [])

  return (
    <div className="app">
      <header className="app-header">
        <h1>sec · 证券行情</h1>
      </header>
      <SearchBar onSearch={onSearch} loading={searching} />
      {error && <div className="error">{error}</div>}
      <div className="content">
        <SecurityList items={results} selected={selected} onSelect={onSelect} />
        <QuoteCard quote={quoteData} />
      </div>
    </div>
  )
}
