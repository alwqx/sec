import { summarizeFiling } from '../api'
import { filingRefLinker } from '../aiSummary'
import type { AIProfileView, FilingSummary as Summary } from '../types'
import SummaryPanel from './SummaryPanel'

// FilingSummary 是申报列表一行的 8-K 事项摘要：每段引用 Item 或 Exhibit，引用链接到对应的原文文件。
export default function FilingSummary({ url, profile, unavailable }: { url: string; profile: AIProfileView | undefined; unavailable: string }) {
  return <SummaryPanel<Summary> label="AI 申报摘要" title="按 Item 和 EX-99 附件生成带引用的事项摘要" busyNote="正在读取 8-K 正文和附件并按事项生成摘要，可能需要数分钟。" openError="打开原文失败，请使用申报原文链接。"
    url={url} profile={profile} unavailable={unavailable} run={summarizeFiling}
    meta={result => `${result.form} · 申报日 ${result.as_of}${result.report_date ? ` · 事项日 ${result.report_date}` : ''}`}
    sections={(result, open) => {
      const links = filingRefLinker(result)
      return result.sections.map((section, i) => <p className="explanation-text" key={i}>{section.text} <span className="data-note">（{links(section.refs).map((ref, j) => <span key={ref.label}>{j > 0 ? '、' : ''}{ref.url
        ? <a href={ref.url} target="_blank" rel="noopener noreferrer" onClick={open(ref.url)}>{ref.label}</a>
        : ref.label}</span>)}）</span></p>)
    }} />
}
