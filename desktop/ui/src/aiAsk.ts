import type { AskStep, BasicSecurity } from './types'

// maxAskQuestion 和 llm.maxAskQuestionRunes 一致，后端还会再校验。
export const maxAskQuestion = 500

// maxStepInput 是工具参数显示的最多字符数，和 sec ask 在终端里显示的一致。
const maxStepInput = 200

// stepLine 是一次工具调用的一行说明：轮次、工具、参数、截止日期或失败原因、耗时，和 sec ask 在 stderr 显示的一致。
// 模型给的参数不是 JSON 时后端按字符串保存，这里和终端一样带引号显示。
export function stepLine(step: AskStep): string {
  let input = JSON.stringify(step.input ?? {})
  if ([...input].length > maxStepInput) input = [...input].slice(0, maxStepInput).join('') + '…'
  let line = `[${step.round}] ${step.tool} ${input}`
  if (step.error) line += ` · 失败：${step.error}`
  else if (step.as_of) line += ` · 截止 ${step.as_of}`
  return `${line} · ${(step.elapsed_ms / 1000).toFixed(1)}s`
}

// askCode 是带入问题的证券上下文：当前研究页的证券代码，用户去掉时为空。
export function askCode(security: BasicSecurity | null, include: boolean): string {
  return include && security ? security.ex_code : ''
}
