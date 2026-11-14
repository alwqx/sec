import type { FilingSummary } from './types'

// summarizableFiling 判断申报能不能摘要：目前只支持 8-K 和修订版 8-K/A，和 sec filings --summary 一致。
export function summarizableFiling(form: string): boolean {
  return form === '8-K' || form === '8-K/A'
}

// filingRefLinker 返回把一段的引用标签对应到原文地址的函数，标签到地址的表每个结果只建一次。
// 引用不在 sources 里时不给链接（后端已校验，这里只防御）。
export function filingRefLinker(result: Pick<FilingSummary, 'sources'>): (refs: string[]) => { label: string; url?: string }[] {
  const urls = new Map(result.sources.map(source => [source.label, source.url]))
  return refs => refs.map(label => ({ label, url: urls.get(label) }))
}
