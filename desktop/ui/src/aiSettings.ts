import type { AIProbeResult, AIProfileInput, AIProfileView, AIProvider } from './types'

const sameURL = (a: string, b: string) => a.replace(/\/+$/, '') === b.replace(/\/+$/, '')

// profileForm 把一个配置换成编辑表单的初始值。显示信息里的协议和地址是实际使用的值，
// 和厂商默认值相同时留空，提交后仍然跟随厂商默认值。
export function profileForm(view: AIProfileView, providers: AIProvider[]): AIProfileInput {
  const provider = providers.find(p => p.id === view.provider)
  return {
    provider: view.provider,
    api: provider && view.api === provider.api ? '' : view.api,
    base_url: provider && sameURL(view.base_url, provider.base_url) ? '' : view.base_url,
    model: view.model,
    api_key_env: view.api_key_env ?? '',
    thinking: view.thinking ?? '',
  }
}

// trimProfile 去掉表单各项首尾的空白，和 sec config ai 一致。
export function trimProfile(form: AIProfileInput): AIProfileInput {
  const out: AIProfileInput = { provider: form.provider.trim(), api: form.api.trim(), base_url: form.base_url.trim(), model: form.model.trim(), api_key_env: form.api_key_env.trim(), thinking: form.thinking.trim() }
  if (form.name !== undefined) out.name = form.name.trim()
  return out
}

// thinkingOffAvailable 判断表单当前的厂商、协议和地址能否关闭思考，规则同 Go 的 config.AIProfile.ThinkingOffSupported：
// 支持的厂商、OpenAI 兼容接口、官方地址，按去掉空白后的值判断。改那边的规则时同步这里。
// 只决定开关能不能打开；保存什么由后端校验决定，两边不一致时最多是后端报错，不会悄悄存成别的值。
export function thinkingOffAvailable(form: AIProfileInput, providers: AIProvider[], supported: string[]): boolean {
  const trimmed = trimProfile(form)
  const provider = providers.find(p => p.id === trimmed.provider)
  if (!provider || !supported.includes(provider.id)) return false
  return (trimmed.api || provider.api) === 'openai' && (trimmed.base_url === '' || sameURL(trimmed.base_url, provider.base_url))
}

// thinkingSwitch 是表单里思考开关的状态：总是显示真实的设置，不在页面里悄悄改掉。不能关闭时只是不能勾选；
// 已经关闭但换到了不支持的组合（conflict）时仍可取消勾选，并提示用户处理，和 CLI 拒绝这种修改一致。
export function thinkingSwitch(form: AIProfileInput, providers: AIProvider[], supported: string[]): { checked: boolean; disabled: boolean; conflict: boolean } {
  const available = thinkingOffAvailable(form, providers, supported)
  const checked = form.thinking === 'off'
  return { checked, disabled: !available && !checked, conflict: checked && !available }
}

// thinkingLabel 是配置列表里思考设置的显示文字，和 sec config ai list 一致。
export function thinkingLabel(view: AIProfileView): string {
  return view.thinking === 'off' ? '关闭' : '默认'
}

// privacyGroups 把配置分成会发给外部厂商的和本机的：remote 带厂商名，供启用前的说明用；remoteNames 只有配置名。
export function privacyGroups(profiles: AIProfileView[]): { remote: string[]; remoteNames: string[]; local: string[] } {
  const remote = profiles.filter(p => !p.local)
  return {
    remote: remote.map(p => `${p.name}（${p.provider_name}）`),
    remoteNames: remote.map(p => p.name),
    local: profiles.filter(p => p.local).map(p => p.name),
  }
}

// withProvider 换厂商：协议和地址清空，回到新厂商的默认值，不把原来的地址带到新厂商。
export function withProvider(form: AIProfileInput, provider: string): AIProfileInput {
  return form.provider === provider ? form : { ...form, provider, api: '', base_url: '' }
}

// removable 判断能不能删除：还有别的配置时不能删除默认配置，要先把别的配置设为默认。
export function removable(view: AIProfileView, count: number): boolean {
  return !view.default || count === 1
}

// probeLines 把连接测试的结果换成显示的几行，和 sec config ai test 的输出一致。
export function probeLines(r: AIProbeResult): string[] {
  const lines = [!r.sent ? '数据：没有发出请求' : r.local ? '数据：本机模型，不出本机' : `数据：发送给 ${r.provider}，只发了一句固定的测试文字，不含你的数据`]
  if (!r.ok) {
    lines.push(`连接失败：${r.error ?? ''}`)
    if (r.hint) lines.push(`建议：${r.hint}`)
    return lines
  }
  const model = r.reply_model && r.reply_model !== r.model ? `，实际模型 ${r.reply_model}` : ''
  lines.push(`连接正常：${(r.latency_ms / 1000).toFixed(1)} 秒${model}，回复「${r.reply ?? ''}」，用量 输入 ${r.usage.input_tokens} / 输出 ${r.usage.output_tokens} tokens`)
  if (r.warning) lines.push(`注意：${r.warning}`)
  switch (r.tools) {
    case 'supported': lines.push('工具调用：支持'); break
    case 'not_called': lines.push(`工具调用：模型没有调用测试工具（${r.tools_detail ?? ''}），这个模型可能不支持工具调用，对话功能会用不了`); break
    case 'error': lines.push(`工具调用：带工具的请求被拒绝（${r.tools_detail ?? ''}），这个模型可能不支持工具调用，对话功能会用不了`); break
    case 'unknown': lines.push(`工具调用：没测出来，带工具的请求临时失败（${r.tools_detail ?? ''}），稍后重试`); break
  }
  return lines
}
