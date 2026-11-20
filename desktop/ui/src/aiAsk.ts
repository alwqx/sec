import type { AskAnswer, AskStep, BasicSecurity } from './types'

// maxAskQuestion 和 llm.maxAskQuestionRunes 一致，后端还会再校验。
export const maxAskQuestion = 500

// maxAskHistory 和 llm.MaxAskHistory 一致，只用于说明。
export const maxAskHistory = 5

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

// answeredSteps 是回答完成后显示的工具调用：同一轮的调用同时执行，逐条收到的是结束顺序；已经全部收到时保留这个顺序，
// 不按回答里的调用顺序重排，免得各行跳动（和 sec ask 在终端显示的一致）。没有全部收到时用回答里的。
export function answeredSteps(streamed: AskStep[], answer: AskAnswer): AskStep[] {
  return streamed.length === answer.steps.length ? streamed : answer.steps
}

// newConversationID 是新对话的 id：32 位十六进制随机数。用 getRandomValues，不依赖只在安全上下文里可用的 randomUUID。
export function newConversationID(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), b => b.toString(16).padStart(2, '0')).join('')
}

// askMeta 是回答上方的一行：请求轮数、工具调用次数，追问时带上了前几轮。
export function askMeta(answer: AskAnswer): string {
  let meta = `请求 ${answer.rounds} 轮 · 调用工具 ${answer.steps.length} 次`
  if (answer.history) meta += ` · 带上前 ${answer.history} 轮问答`
  return meta
}

// askCode 是带入问题的证券上下文：当前研究页的证券代码，用户去掉时为空。
export function askCode(security: BasicSecurity | null, include: boolean): string {
  return include && security ? security.ex_code : ''
}
