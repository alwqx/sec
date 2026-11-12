import { useEffect, useRef, useState } from 'react'
import { summarizeAnnouncement } from '../api'
import { explanationChecks } from '../aiValuation'
import type { AIProfileView, AnnouncementSummary as Summary } from '../types'

export default function AnnouncementSummary({ url, profile, unavailable }: { url: string; profile: AIProfileView | undefined; unavailable: string }) {
  const [result, setResult] = useState<Summary | null>(null)
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
  }, [url, profile, unavailable])
  const cancel = () => { controller.current?.abort(); controller.current = null; setBusy(false) }
  const generate = async () => {
    if (!profile || unavailable || busy) return
    const request = new AbortController()
    controller.current = request
    setBusy(true)
    setResult(null)
    setError('')
    try {
      const summary = await summarizeAnnouncement(url, profile.name, request.signal)
      if (!request.signal.aborted) setResult(summary)
    } catch (e) {
      if (!request.signal.aborted) setError(e instanceof Error ? e.message : String(e))
    } finally {
      if (controller.current === request) { controller.current = null; setBusy(false) }
    }
  }
  return <div className="announcement-summary" aria-label="AI 公告摘要">
    <div className="announcement-actions">
      <button disabled={!profile || !!unavailable || busy} title={unavailable || '根据 PDF 原文生成带页码的摘要'} onClick={() => void generate()}>{busy ? '摘要中…' : result ? '重新生成' : '摘要'}</button>
      {busy && <button onClick={cancel}>取消摘要</button>}
    </div>
    {busy && <p className="data-note" role="status">正在读取 PDF 并按页分块生成摘要，长文档可能需要数分钟。</p>}
    {error && <p className="error" role="alert">{error}</p>}
    {result && !unavailable && <div className="explanation-result" role="status">
      <p className="data-note">披露日期 {result.as_of} · PDF {result.page_count} 页 · 模型 {result.model} · 配置 {result.profile}{result.cached ? ` · 缓存，生成于 ${new Date(result.created_at).toLocaleString('zh-CN')}` : ''}</p>
      {result.sections.map((section, i) => <p className="explanation-text" key={i}>{section.text} <span className="data-note">（PDF {section.pages.map((page, j) => <span key={page}>{j > 0 ? '、' : ''}<a href={`${result.source_url}#page=${page}`} target="_blank" rel="noopener noreferrer" onClick={event => {
        if (!window.secOpenDisclosure) return
        event.preventDefault()
        void window.secOpenDisclosure(`${result.source_url}#page=${page}`).catch(() => setError('打开原文失败，请使用公告原文链接。'))
      }}>第 {page} 页</a></span>)}）</span></p>)}
      {explanationChecks(result).map(line => <p key={line} className={result.check.unsourced.length || result.check.advice_terms.length ? 'error' : 'data-note'}>核对：{line}</p>)}
      {result.notes.map(note => <p key={note} className="data-note">{note}</p>)}
      <p className="data-note">{result.disclaimer}</p>
    </div>}
  </div>
}
