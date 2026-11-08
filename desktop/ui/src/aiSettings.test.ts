import assert from 'node:assert/strict'
import test from 'node:test'
import type { AIProfileView, AIProvider } from './types'
import { privacyGroups, profileForm, removable, trimProfile, withProvider } from './aiSettings.ts'

const providers: AIProvider[] = [
  { id: 'deepseek', name: 'DeepSeek', api: 'openai', base_url: 'https://api.deepseek.com', key_env: 'DEEPSEEK_API_KEY' },
  { id: 'custom', name: '自定义', api: '', base_url: '' },
]
const view = (over: Partial<AIProfileView>): AIProfileView => ({ name: 'ds', default: true, provider: 'deepseek', provider_name: 'DeepSeek', api: 'openai', base_url: 'https://api.deepseek.com', model: 'deepseek-chat', key: '未设置', local: false, ...over })

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
