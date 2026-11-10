import assert from 'node:assert/strict'
import test from 'node:test'
import { explanationChecks, explanationInput, explanationProfile, explanationUnavailable } from './aiValuation.ts'
import type { AIProfileView, AISettings, ValuationExplanation } from './types'

const first: AIProfileView = { name: 'ds', default: true, provider: 'deepseek', provider_name: 'DeepSeek', api: 'openai', base_url: 'https://api.deepseek.com', model: 'deepseek-chat', key: '未设置', key_stored: false, local: false }
const second: AIProfileView = { ...first, name: 'local', default: false, provider: 'ollama', local: true }
const settings: AISettings = { enabled: true, default: 'ds', profiles: [first, second], providers: [] }

test('解读请求省略空参数、保留零值并使用 get_valuation 的参数名', () => {
  assert.deepEqual(explanationInput('SH600036', 'ds', { growth: '', terminal: '', wacc: ' ', margin: '' }), { code: 'SH600036', profile: 'ds' })
  assert.deepEqual(explanationInput('$AAPL', 'local', { growth: '0', terminal: '0', wacc: ' 9 ', margin: '0' }), { code: '$AAPL', profile: 'local', growth: 0, terminal_growth: 0, wacc: 9, margin_of_safety: 0 })
  for (const value of ['NaN', 'Infinity', 'abc']) assert.throws(() => explanationInput('SH600036', 'ds', { wacc: value }), /有效数字/)
})

test('模型选择默认使用 default，临时切换不修改共享配置，删除后回到 default', () => {
  assert.equal(explanationProfile(settings, ''), first)
  assert.equal(explanationProfile(settings, 'local'), second)
  assert.equal(settings.default, 'ds')
  assert.equal(explanationProfile(settings, 'deleted'), first)
  assert.equal(explanationProfile(null, ''), undefined)
})

test('未配置、未启用或配置无效时提示去设置页，不允许生成', () => {
  assert.match(explanationUnavailable(null), /正在读取/)
  assert.match(explanationUnavailable({ ...settings, profiles: [] }), /添加并启用/)
  assert.match(explanationUnavailable({ ...settings, enabled: false }), /尚未启用/)
  assert.match(explanationUnavailable({ ...settings, invalid: '无效原因' }), /无效原因/)
  assert.equal(explanationUnavailable(settings), '')
})

const result = (check: ValuationExplanation['check']): ValuationExplanation => ({ ex_code: 'SH600036', name: '招商银行', as_of: '2026-10-30', text: '解读', disclaimer: '免责', model: 'm', profile: 'ds', prompt_version: 'v1', cached: false, created_at: '2026-11-09T00:00:00Z', truncated: false, check })

test('解读核对结果保留未找到的数字和建议用语，不把没有数字说成通过', () => {
  const ref = (text: string) => ({ text, start: 0, end: text.length })
  assert.deepEqual(explanationChecks(result({ numbers: 0, unsourced: [], advice_terms: [] })), ['回答里没有数字，未核对'])
  assert.deepEqual(explanationChecks(result({ numbers: 3, unsourced: [], advice_terms: [] })), ['3 个数字都能在数据中找到'])
  const lines = explanationChecks(result({ numbers: 3, unsourced: [ref('99%'), ref('99%')], advice_terms: [ref('买入')] }))
  assert.deepEqual(lines, ['以下数字未在数据中找到：99%', '回答里出现了投资建议用语：买入；sec 只解释数据，不构成投资建议'])
})
