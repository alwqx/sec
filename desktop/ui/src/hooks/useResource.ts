import { useEffect, useState } from 'react'

// 每次切换资源时取消旧请求，不把上一只证券或上一页的结果显示到当前页面。
// 调用者使用 useCallback 稳定 load 的身份。
export function useResource<T>(load: (signal: AbortSignal) => Promise<T>) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    setData(null)
    setLoading(true)
    setError('')
    void load(controller.signal).then(value => {
      if (!controller.signal.aborted) setData(value)
    }).catch((e: unknown) => {
      if (!controller.signal.aborted) setError(e instanceof Error ? e.message : String(e))
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false)
    })
    return () => controller.abort()
  }, [load, retry])
  return { data, error, loading, reload: () => setRetry(n => n + 1) }
}
