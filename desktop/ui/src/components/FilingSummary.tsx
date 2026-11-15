import { summarizeFiling } from '../api'
import { filingRefLinker, isEightK, reportDateLabel } from '../aiSummary'
import type { AIProfileView, FilingSummary as Summary } from '../types'
import SummaryPanel from './SummaryPanel'

// FilingSummary 是申报列表一行的 AI 摘要：8-K 按 Item 和 EX-99 附件，10-K 按 Item 1A 风险因素和 Item 7 管理层讨论与分析；
// 每段的引用链接到对应的原文文件。form 是列表里的类型，用来写按钮提示和进度说明。
export default function FilingSummary({ url, form, profile, unavailable }: { url: string; form: string; profile: AIProfileView | undefined; unavailable: string }) {
  const eightK = isEightK(form)
  return <SummaryPanel<Summary> label="AI 申报摘要" openError="打开原文失败，请使用申报原文链接。"
    title={eightK ? '按 Item 和 EX-99 附件生成带引用的事项摘要' : '按风险因素和管理层讨论与分析生成带引用的摘要'}
    busyNote={eightK ? '正在读取 8-K 正文和附件并按事项生成摘要，可能需要数分钟。' : '正在读取 10-K 的风险因素和管理层讨论与分析并分块生成摘要，可能需要数分钟。'}
    url={url} profile={profile} unavailable={unavailable} run={summarizeFiling}
    meta={result => `${result.form} · 申报日 ${result.as_of}${result.report_date ? ` · ${reportDateLabel(result.form)} ${result.report_date}` : ''}`}
    sections={(result, open) => {
      const links = filingRefLinker(result)
      return result.sections.map((section, i) => <p className="explanation-text" key={i}>{section.text} <span className="data-note">（{links(section.refs).map((ref, j) => <span key={ref.label}>{j > 0 ? '、' : ''}{ref.url
        ? <a href={ref.url} target="_blank" rel="noopener noreferrer" onClick={open(ref.url)}>{ref.label}</a>
        : ref.label}</span>)}）</span></p>)
    }} />
}
