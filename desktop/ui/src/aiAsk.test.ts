import assert from 'node:assert/strict'
import test from 'node:test'
import { askCode, askMeta, newConversationID, stepLine } from './aiAsk.ts'
import type { AskAnswer, AskStep, BasicSecurity } from './types'

const step: AskStep = { round: 1, tool: 'get_quote', input: { codes: ['600036'] }, as_of: '2026-11-17', elapsed_ms: 340 }

test('工具调用一行显示轮次、参数、截止日期或失败原因和耗时，和 sec ask 一致', () => {
  assert.equal(stepLine(step), '[1] get_quote {"codes":["600036"]} · 截止 2026-11-17 · 0.3s')
  assert.equal(stepLine({ ...step, as_of: '', error: '缺少 code' }), '[1] get_quote {"codes":["600036"]} · 失败：缺少 code · 0.3s')
  assert.equal(stepLine({ ...step, tool: 'search_security', input: { query: '招商银行' }, as_of: undefined }), '[1] search_security {"query":"招商银行"} · 0.3s', '搜索结果没有截止日期')
  assert.equal(stepLine({ ...step, input: 'not json', as_of: undefined }), '[1] get_quote "not json" · 0.3s', '不合法的参数按字符串带引号显示')
  assert.match(stepLine({ ...step, input: { query: '长'.repeat(300) } }), /^\[1\] get_quote \{"query":"长{190}… · 截止/, '参数最多显示 200 个字符')
})

test('研究页的证券默认带入，去掉或没有选中时为空', () => {
  const security = { ex_code: 'SH600036', name: '招商银行' } as BasicSecurity
  assert.equal(askCode(security, true), 'SH600036')
  assert.equal(askCode(security, false), '')
  assert.equal(askCode(null, true), '')
})

test('新对话的 id 是 32 位十六进制，每次不同', () => {
  const id = newConversationID()
  assert.match(id, /^[0-9a-f]{32}$/)
  assert.notEqual(newConversationID(), id)
})

test('回答上方显示请求轮数、工具调用次数，追问时显示带上了前几轮', () => {
  const answer = { rounds: 2, steps: [step] } as unknown as AskAnswer
  assert.equal(askMeta(answer), '请求 2 轮 · 调用工具 1 次')
  assert.equal(askMeta({ ...answer, history: 3 }), '请求 2 轮 · 调用工具 1 次 · 带上前 3 轮问答')
})
