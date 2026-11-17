import { useEffect, useState, type FormEvent } from 'react'
import { askProspectus } from '../api'
import { openDisclosure } from '../disclosure'
import { useSummaryRequest } from '../hooks/useSummaryRequest'
import type { AIProfileView, ProspectusAnswer } from '../types'
import { PDFSections } from './AnnouncementSummary'
import { SummaryResultView } from './SummaryPanel'

// maxQuestion 和 llm.maxQuestionRunes 一致，后端还会再校验。
const maxQuestion = 200

// ProspectusAsk 是招股书列表一行的 AI 问答框：按问题挑出相关的页交给模型回答，每段引用 PDF 页码，和 sec ipo ask 一致。
// 招股书链接或所选配置名变化时取消请求并清除旧回答；改问题不清除，提问时才换成新的回答。
// onBusy 告诉列表是否正在回答，列表据此禁用刷新和翻页，避免卸载这一行时悄悄取消要好几分钟的回答。
export default function ProspectusAsk({ url, profile, unavailable, onBusy }: { url: string; profile: AIProfileView | undefined; unavailable: string; onBusy?: (busy: boolean) => void }) {
  const [question, setQuestion] = useState('')
  const { result, error, setError, busy, generate, cancel } = useSummaryRequest<ProspectusAnswer>(signal => askProspectus(url, question, profile!.name, signal), [url, profile?.name])
  useEffect(() => {
    if (!busy) return
    onBusy?.(true)
    return () => onBusy?.(false)
  }, [busy])
  const blocked = !profile || !!unavailable
  const open = openDisclosure(() => setError('打开原文失败，请使用招股书原文链接。'))
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!blocked && !busy && question.trim()) void generate()
  }
  return <div className="announcement-summary" aria-label="AI 招股书问答">
    <form className="announcement-actions prospectus-ask" onSubmit={submit}>
      <input aria-label="招股书问题" value={question} maxLength={maxQuestion} disabled={blocked} placeholder="如：最近三年的营业收入和净利润是多少？" onChange={event => setQuestion(event.target.value)} />
      <button type="submit" disabled={blocked || busy || !question.trim()} title={unavailable || '按问题挑出相关的页，生成带页码的回答'}>{busy ? '回答中…' : '提问'}</button>
      {busy && <button type="button" onClick={cancel}>取消提问</button>}
    </form>
    {busy && <p className="data-note" role="status">正在按问题挑选相关的页并请求模型回答。第一次问这份招股书要下载和解析整份 PDF，可能需要数分钟。</p>}
    {error && <p className="error" role="alert">{error}</p>}
    {result && <SummaryResultView result={result} meta={`问题：${result.question} · 披露日期 ${result.as_of} · 从 ${result.page_count} 页中挑出 ${result.pages.length} 页`}>
      <PDFSections sourceURL={result.source_url} sections={result.sections} open={open} />
    </SummaryResultView>}
  </div>
}
