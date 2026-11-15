import { useState } from 'react'
import { explainRisks, filingRisks } from '../api'
import { openDisclosure } from '../disclosure'
import { useSummaryRequest } from '../hooks/useSummaryRequest'
import type { AIProfileView, RiskChanges as Changes, RiskExplanation, RiskFactor } from '../types'
import SummaryPanel from './SummaryPanel'

// RiskChanges 是申报列表 10-K 行的风险因素逐年对比：先确定性地列出新增、删除和改写，不调用模型；有变化时可以再用模型解读，
// 解读每段的引用（如 新增 1、改写 3）对应列表里的序号。和 sec filings --risks、--risks --explain 一致。
export default function RiskChanges({ url, profile, unavailable }: { url: string; profile: AIProfileView | undefined; unavailable: string }) {
  const { result, error, busy, generate, cancel } = useSummaryRequest<Changes>(signal => filingRisks(url, signal), [url])
  const [openError, setOpenError] = useState('')
  const open = openDisclosure(() => setOpenError('打开原文失败，请复制链接到浏览器。'))
  const changed = !!result && result.added.length + result.removed.length + result.revised.length > 0
  return <div className="announcement-summary" aria-label="风险因素变化">
    {/* 对比是确定的，结果出来后不再提供按钮：重新对比会清掉列表和下面已经生成的解读。失败时可以重试。 */}
    {!result && <div className="announcement-actions">
      <button disabled={busy} title="和上一年的 10-K 对比风险因素小标题，不调用模型" onClick={() => { setOpenError(''); void generate() }}>{busy ? '对比中…' : '风险因素变化'}</button>
      {busy && <button onClick={cancel}>取消对比</button>}
    </div>}
    {busy && <p className="data-note" role="status">正在读取今年和上一年的 10-K 并对比风险因素，可能需要一分钟。</p>}
    {error && <p className="error" role="alert">{error}</p>}
    {openError && <p className="error" role="alert">{openError}</p>}
    {result && <div className="explanation-result" role="status">
      <p className="data-note">{result.current.form} 报告期 {result.current.report_date} 对比上一年 {result.previous.form} 报告期 {result.previous.report_date}（<a href={result.previous.url} target="_blank" rel="noopener noreferrer" onClick={open(result.previous.url)}>上一年原文 ↗</a>）· 上一年 {result.previous.count} 条，今年 {result.current.count} 条</p>
      <RiskList label="新增" risks={result.added} />
      <RiskList label="删除" risks={result.removed} />
      <p className="explanation-text"><strong>改写（{result.revised.length}）</strong>{result.revised.length === 0 && ' 无'}</p>
      {result.revised.length > 0 && <ol>{result.revised.map((r, i) => <li key={i}>{category(r.new)}新：{r.new.title}<br /><span className="data-note">旧：{r.old.title}（小标题相似度 {r.title_similarity.toFixed(2)}，正文 {r.text_similarity.toFixed(2)}）</span></li>)}</ol>}
      {result.kept.length > 0 && <p className="data-note">保留的 {result.kept.length} 条小标题相同。</p>}
      {result.notes.map(note => <p key={note} className="data-note">{note}</p>)}
    </div>}
    {changed && <SummaryPanel<RiskExplanation> label="AI 风险因素变化解读" verb="解读" title="把上面的新增、删除和改写交给模型解释，模型不找差异"
      busyNote="正在把变化交给模型解读，改写很多时可能需要数分钟。" openError="打开原文失败，请复制链接到浏览器。"
      url={url} profile={profile} unavailable={unavailable} run={explainRisks}
      meta={explanation => `${explanation.changes.current.form} 报告期 ${explanation.changes.current.report_date} 对比 ${explanation.changes.previous.report_date}`}
      sections={explanation => explanation.sections.map((section, i) => <p className="explanation-text" key={i}>{section.text} <span className="data-note">（{section.refs.join('、')}）</span></p>)} />}
  </div>
}

function RiskList({ label, risks }: { label: string; risks: RiskFactor[] }) {
  return <>
    <p className="explanation-text"><strong>{label}（{risks.length}）</strong>{risks.length === 0 && ' 无'}</p>
    {risks.length > 0 && <ol>{risks.map((r, i) => <li key={i}>{category(r)}{r.title}</li>)}</ol>}
  </>
}

// category 返回「[类别] 」，没有类别时为空。
function category(r: RiskFactor): string {
  return r.category ? `[${r.category}] ` : ''
}
