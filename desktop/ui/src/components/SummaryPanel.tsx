import type { ReactNode } from 'react'
import { explanationChecks } from '../aiValuation'
import { openDisclosure } from '../disclosure'
import { useSummaryRequest } from '../hooks/useSummaryRequest'
import type { AIProfileView, ValuationExplanation } from '../types'

// SummaryResult 是公告和申报摘要、招股书问答共有的字段。
export interface SummaryResult {
  check: ValuationExplanation['check']
  notes: string[]
  disclaimer: string
  model: string
  profile: string
  cached: boolean
  created_at: string
}

// SummaryPanel 是列表一行的 AI 摘要或解读：按钮、取消、进度、错误，以及结果的核对、说明和免责声明，公告和申报摘要、
// 风险因素变化解读共用。
// 文档链接或所选配置名变化时取消请求并清除旧结果；刷新模型配置不会重置。meta 是结果第一行的文档信息，
// sections 渲染各段正文和引用，open 用 Desktop 外部窗口打开原文链接。
export default function SummaryPanel<T extends SummaryResult>({ label, title, busyNote, openError, url, profile, unavailable, run, meta, sections, verb = '摘要' }: {
  label: string
  title: string
  busyNote: string
  openError: string
  url: string
  profile: AIProfileView | undefined
  unavailable: string
  run: (url: string, profile: string, signal: AbortSignal) => Promise<T>
  meta: (result: T) => string
  sections: (result: T, open: (link: string) => (event: { preventDefault: () => void }) => void) => ReactNode
  // verb 是按钮上的动作，如「摘要」「解读」。
  verb?: string
}) {
  const { result, error, setError, busy, generate, cancel } = useSummaryRequest<T>(signal => run(url, profile!.name, signal), [url, profile?.name])
  const blocked = !profile || !!unavailable
  const open = openDisclosure(() => setError(openError))
  return <div className="announcement-summary" aria-label={label}>
    <div className="announcement-actions">
      <button disabled={blocked || busy} title={unavailable || title} onClick={() => { if (!blocked) void generate() }}>{busy ? `${verb}中…` : result ? '重新生成' : verb}</button>
      {busy && <button onClick={cancel}>取消{verb}</button>}
    </div>
    {busy && <p className="data-note" role="status">{busyNote}</p>}
    {error && <p className="error" role="alert">{error}</p>}
    {result && <SummaryResultView result={result} meta={meta(result)}>{sections(result, open)}</SummaryResultView>}
  </div>
}

// SummaryResultView 是摘要、解读和招股书问答结果的外框：第一行的文档信息、模型和缓存，children 是各段正文，
// 后面是核对结果、说明和免责声明。
export function SummaryResultView({ result, meta, children }: { result: SummaryResult; meta: string; children: ReactNode }) {
  return <div className="explanation-result" role="status">
    <p className="data-note">{meta} · 模型 {result.model} · 配置 {result.profile}{result.cached ? ` · 缓存，生成于 ${new Date(result.created_at).toLocaleString('zh-CN')}` : ''}</p>
    {children}
    {explanationChecks(result).map(line => <p key={line} className={result.check.unsourced.length || result.check.advice_terms.length ? 'error' : 'data-note'}>核对：{line}</p>)}
    {result.notes.map(note => <p key={note} className="data-note">{note}</p>)}
    <p className="data-note">{result.disclaimer}</p>
  </div>
}
