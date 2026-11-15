import type { FilingSummary } from './types'

// baseForm 去掉修订版的 /A，如 10-K/A 是 10-K。
function baseForm(form: string): string {
  return form.endsWith('/A') ? form.slice(0, -2) : form
}

// isEightK 判断申报是不是 8-K 或 8-K/A。
export function isEightK(form: string): boolean {
  return baseForm(form) === '8-K'
}

// annualReport 判断申报是不是原始的 10-K 或过渡期年报 10-KT。修订版 10-K/A 多半只补充第三部分，没有风险因素和 MD&A，
// 按钮几乎一定失败，所以 Desktop 不给修订版年报按钮；CLI 仍然接受，有的修订版是完整重述。
function annualReport(form: string): boolean {
  return form === '10-K' || form === '10-KT'
}

// summarizableFiling 判断申报行有没有摘要按钮：8-K（含修订版）、原始 10-K 和 10-KT。
export function summarizableFiling(form: string): boolean {
  return isEightK(form) || annualReport(form)
}

// riskFiling 判断申报行有没有风险因素逐年对比：原始 10-K 和 10-KT。
export function riskFiling(form: string): boolean {
  return annualReport(form)
}

// reportDateLabel 是摘要里 report_date 的叫法：8-K 是最早事项的发生日，10-K 是财年截止日。
export function reportDateLabel(form: string): string {
  return isEightK(form) ? '事项日' : '报告期'
}

// summaryPrivacy 是申报列表上方隐私说明的两部分：sends 是点击摘要会发给模型的内容，extra 是附加说明，按页面上实际有的申报类型写。
export function summaryPrivacy(forms: string[]): { sends: string; extra: string } {
  const eightK = forms.some(isEightK), tenK = forms.some(annualReport)
  const sends = [eightK && '8-K 正文和 EX-99 附件', tenK && '10-K 风险因素和管理层讨论与分析'].filter(Boolean).join('、') + '的文本'
  const extra = [eightK && '8-K 摘要引用对应 Item 和附件。', tenK && '10-K 摘要引用 Item 1A 和 Item 7；风险因素变化在本机对比，不调用模型，点「解读」才把变化发给模型。'].filter(Boolean).join('')
  return { sends, extra }
}

// filingRefLinker 返回把一段的引用标签对应到原文地址的函数，标签到地址的表每个结果只建一次。
// 引用不在 sources 里时不给链接（后端已校验，这里只防御）。
export function filingRefLinker(result: Pick<FilingSummary, 'sources'>): (refs: string[]) => { label: string; url?: string }[] {
  const urls = new Map(result.sources.map(source => [source.label, source.url]))
  return refs => refs.map(label => ({ label, url: urls.get(label) }))
}
