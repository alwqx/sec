import type { AIProfileInput, AIProfileView, AIProvider } from './types'

// profileForm 把一个配置换成编辑表单的初始值。显示信息里的协议和地址是实际使用的值，
// 和厂商默认值相同时留空，提交后仍然跟随厂商默认值。
export function profileForm(view: AIProfileView, providers: AIProvider[]): AIProfileInput {
  const provider = providers.find(p => p.id === view.provider)
  const sameURL = (a: string, b: string) => a.replace(/\/+$/, '') === b.replace(/\/+$/, '')
  return {
    provider: view.provider,
    api: provider && view.api === provider.api ? '' : view.api,
    base_url: provider && sameURL(view.base_url, provider.base_url) ? '' : view.base_url,
    model: view.model,
    api_key_env: view.api_key_env ?? '',
  }
}

// trimProfile 去掉表单各项首尾的空白，和 sec config ai 一致。
export function trimProfile(form: AIProfileInput): AIProfileInput {
  const out: AIProfileInput = { provider: form.provider.trim(), api: form.api.trim(), base_url: form.base_url.trim(), model: form.model.trim(), api_key_env: form.api_key_env.trim() }
  if (form.name !== undefined) out.name = form.name.trim()
  return out
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
