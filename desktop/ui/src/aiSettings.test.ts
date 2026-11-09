import assert from 'node:assert/strict'
import test from 'node:test'
import type { AIProbeResult, AIProfileView, AIProvider } from './types'
import { privacyGroups, probeLines, profileForm, removable, trimProfile, withProvider } from './aiSettings.ts'

const providers: AIProvider[] = [
  { id: 'deepseek', name: 'DeepSeek', api: 'openai', base_url: 'https://api.deepseek.com', key_env: 'DEEPSEEK_API_KEY' },
  { id: 'custom', name: '自定义', api: '', base_url: '' },
]
const view = (over: Partial<AIProfileView>): AIProfileView => ({ name: 'ds', default: true, provider: 'deepseek', provider_name: 'DeepSeek', api: 'openai', base_url: 'https://api.deepseek.com', model: 'deepseek-chat', key: '未设置', key_stored: false, local: false, ...over })

test('profileForm 和厂商默认值相同的协议和地址留空，跟随默认值', () => {
  assert.deepEqual(profileForm(view({}), providers), { provider: 'deepseek', api: '', base_url: '', model: 'deepseek-chat', api_key_env: '' })
  assert.equal(profileForm(view({ base_url: 'https://api.deepseek.com/' }), providers).base_url, '', '结尾的斜杠不算不同')
})

test('profileForm 保留改过的协议、地址和环境变量', () => {
  const form = profileForm(view({ api: 'anthropic', base_url: 'https://api.deepseek.com/anthropic', api_key_env: 'MY_KEY' }), providers)
  assert.equal(form.api, 'anthropic')
  assert.equal(form.base_url, 'https://api.deepseek.com/anthropic')
  assert.equal(form.api_key_env, 'MY_KEY')
  const custom = profileForm(view({ provider: 'custom', api: 'openai', base_url: 'https://gw.example.com/v1' }), providers)
  assert.equal(custom.api, 'openai', '自定义厂商没有默认值，都保留')
  assert.equal(custom.base_url, 'https://gw.example.com/v1')
})

test('trimProfile 去掉首尾空白，编辑时不带名字', () => {
  assert.deepEqual(trimProfile({ name: ' ds ', provider: 'deepseek', api: '', base_url: ' https://x ', model: ' m ', api_key_env: '' }), { name: 'ds', provider: 'deepseek', api: '', base_url: 'https://x', model: 'm', api_key_env: '' })
  assert.equal('name' in trimProfile({ provider: 'deepseek', api: '', base_url: '', model: 'm', api_key_env: '' }), false)
})

test('privacyGroups 区分外部厂商和本机模型', () => {
  assert.deepEqual(privacyGroups([view({}), view({ name: 'local', provider_name: 'Ollama（本地）', local: true })]), { remote: ['ds（DeepSeek）'], remoteNames: ['ds'], local: ['local'] })
})

test('withProvider 换厂商时清空协议和地址，没换时不变', () => {
  const form = { provider: 'custom', api: 'anthropic', base_url: 'https://gw.example/v1', model: 'm', api_key_env: 'K' }
  assert.deepEqual(withProvider(form, 'deepseek'), { provider: 'deepseek', api: '', base_url: '', model: 'm', api_key_env: 'K' })
  assert.equal(withProvider(form, 'custom'), form)
})

test('removable 还有别的配置时不能删除默认配置', () => {
  assert.equal(removable(view({ default: true }), 2), false)
  assert.equal(removable(view({ default: true }), 1), true, '只剩一个时可以删除')
  assert.equal(removable(view({ default: false }), 2), true)
})

const probe = (over: Partial<AIProbeResult>): AIProbeResult => ({ name: 'ds', provider: 'DeepSeek', api: 'openai', base_url: 'https://api.deepseek.com', model: 'deepseek-chat', key: '加密保存', local: false, ok: true, sent: true, latency_ms: 1234, reply_model: 'deepseek-chat', reply: 'OK', usage: { input_tokens: 10, output_tokens: 2 }, tools: 'supported', ...over })

test('probeLines 连接正常时列出耗时、用量和工具调用', () => {
  assert.deepEqual(probeLines(probe({})), ['数据：发送给 DeepSeek，只发了一句固定的测试文字，不含你的数据', '连接正常：1.2 秒，回复「OK」，用量 输入 10 / 输出 2 tokens', '工具调用：支持'])
  assert.match(probeLines(probe({ reply_model: 'deepseek-v4' }))[1], /实际模型 deepseek-v4/)
  assert.match(probeLines(probe({ tools: 'unknown', tools_detail: '429' })).at(-1)!, /没测出来/)
  assert.equal(probeLines(probe({ local: true }))[0], '数据：本机模型，不出本机')
})

test('probeLines 失败时给出原因和建议，没发请求时说明', () => {
  assert.deepEqual(probeLines(probe({ ok: false, sent: false, error: '没有 API Key', hint: '' })), ['数据：没有发出请求', '连接失败：没有 API Key'])
  assert.deepEqual(probeLines(probe({ ok: false, error: 'HTTP 401', hint: 'Key 不对' })).slice(1), ['连接失败：HTTP 401', '建议：Key 不对'])
})

test('probeLines 区分工具请求被拒绝、未调用、临时失败并保留警告', () => {
  assert.match(probeLines(probe({ tools: 'error', tools_detail: 'HTTP 400' })).at(-1)!, /带工具的请求被拒绝（HTTP 400）/)
  assert.match(probeLines(probe({ tools: 'not_called', tools_detail: '停止原因 stop' })).at(-1)!, /模型没有调用测试工具（停止原因 stop）/)
  assert.match(probeLines(probe({ tools: 'unknown', tools_detail: 'HTTP 503' })).at(-1)!, /没测出来.*HTTP 503.*稍后重试/)
  assert.deepEqual(probeLines(probe({ warning: '回复被截断', tools: undefined })).slice(2), ['注意：回复被截断'])
})
