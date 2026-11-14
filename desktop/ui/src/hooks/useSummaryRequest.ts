import { useEffect, useRef, useState } from 'react'

// useSummaryRequest 管理一次摘要请求：生成、取消，以及 resetKeys（文档链接、所选配置名）变化时取消请求并清除旧结果，
// 旧响应不会覆盖新页面。resetKeys 用配置名而不是配置对象：刷新模型配置会得到新对象，不能因此丢掉正在进行的摘要。
export function useSummaryRequest<T>(run: (signal: AbortSignal) => Promise<T>, resetKeys: unknown[]) {
  const [result, setResult] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const controller = useRef<AbortController | null>(null)
  useEffect(() => {
    controller.current?.abort()
    controller.current = null
    setResult(null)
    setError('')
    setBusy(false)
    return () => controller.current?.abort()
  }, resetKeys)
  const cancel = () => { controller.current?.abort(); controller.current = null; setBusy(false) }
  const generate = async () => {
    if (busy) return
    const request = new AbortController()
    controller.current = request
    setBusy(true)
    setResult(null)
    setError('')
    try {
      const value = await run(request.signal)
      if (!request.signal.aborted) setResult(value)
    } catch (e) {
      if (!request.signal.aborted) setError(e instanceof Error ? e.message : String(e))
    } finally {
      if (controller.current === request) { controller.current = null; setBusy(false) }
    }
  }
  return { result, error, setError, busy, generate, cancel }
}
