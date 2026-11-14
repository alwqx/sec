import { summarizeAnnouncement } from '../api'
import type { AIProfileView, AnnouncementSummary as Summary } from '../types'
import SummaryPanel from './SummaryPanel'

// AnnouncementSummary 是公告列表一行的 AI 摘要：每段引用物理 PDF 页码，页码链接到原文对应页。
export default function AnnouncementSummary({ url, profile, unavailable }: { url: string; profile: AIProfileView | undefined; unavailable: string }) {
  return <SummaryPanel<Summary> label="AI 公告摘要" title="根据 PDF 原文生成带页码的摘要" busyNote="正在读取 PDF 并按页分块生成摘要，长文档可能需要数分钟。" openError="打开原文失败，请使用公告原文链接。"
    url={url} profile={profile} unavailable={unavailable} run={summarizeAnnouncement}
    meta={result => `披露日期 ${result.as_of} · PDF ${result.page_count} 页`}
    sections={(result, open) => result.sections.map((section, i) => <p className="explanation-text" key={i}>{section.text} <span className="data-note">（PDF {section.pages.map((page, j) => <span key={page}>{j > 0 ? '、' : ''}<a href={`${result.source_url}#page=${page}`} target="_blank" rel="noopener noreferrer" onClick={open(`${result.source_url}#page=${page}`)}>第 {page} 页</a></span>)}）</span></p>)} />
}
