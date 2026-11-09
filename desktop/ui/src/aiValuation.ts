import type { AISettings, ValuationExplanation } from './types'

export interface ValuationExplanationInput {
  code: string
  profile: string
  growth?: number
  terminal_growth?: number
  wacc?: number
  margin_of_safety?: number
}

// 只发送已应用的 DCF 参数；留空时省略，0 保留，与 CLI 共用参数及缓存键。
export function explanationInput(code: string, profile: string, params: Record<string, string>): ValuationExplanationInput {
  const input: ValuationExplanationInput = { code, profile }
  for (const [field, key] of [['growth', 'growth'], ['terminal', 'terminal_growth'], ['wacc', 'wacc'], ['margin', 'margin_of_safety']] as const) {
    const text = params[field]?.trim()
    if (!text) continue
    const value = Number(text)
    if (!Number.isFinite(value)) throw new Error('DCF 参数须为有效数字，请重新应用参数')
    input[key] = value
  }
  return input
}

// 没有主动切换时选 default；刷新后原配置被删除时回到当前 default，不写共享配置。
export function explanationProfile(settings: AISettings | null, selected: string) {
  return settings?.profiles.find(p => p.name === selected) ?? settings?.profiles.find(p => p.name === settings.default)
}

export function explanationUnavailable(settings: AISettings | null): string {
  if (!settings) return '正在读取模型配置…'
  if (settings.invalid) return `模型配置无效，请到设置页修正：${settings.invalid}`
  if (!settings.profiles.length) return '还没有模型配置，请到设置页「AI 模型」添加并启用。'
  if (!settings.enabled) return 'AI 功能尚未启用，请到设置页「AI 模型」启用。'
  return ''
}

// 与 llm.Check.Warnings 的提示一致，不把没有数字的回答说成核对通过。
export function explanationChecks(result: ValuationExplanation): string[] {
  const lines: string[] = []
  const numbers = [...new Set(result.check.unsourced.map(item => item.text))]
  const advice = [...new Set(result.check.advice_terms.map(item => item.text))]
  if (numbers.length) lines.push(`以下数字未在数据中找到：${numbers.join('、')}`)
  if (advice.length) lines.push(`回答里出现了投资建议用语：${advice.join('、')}；sec 只解释数据，不构成投资建议`)
  if (!lines.length) lines.push(result.check.numbers ? `${result.check.numbers} 个数字都能在数据中找到` : '回答里没有数字，未核对')
  return lines
}
