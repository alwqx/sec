import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { aiSettings, changeAI, testAIProfile, type AIChange } from '../api'
import { privacyGroups, probeLines, profileForm, removable, thinkingLabel, thinkingSwitch, trimProfile, withProvider } from '../aiSettings'
import { useResource } from '../hooks/useResource'
import type { AIProbeResult, AIProfileInput, AIProvider, AISettings as Settings } from '../types'
import ResourceState from './ResourceState'

const emptyForm: AIProfileInput = { name: '', provider: 'deepseek', api: '', base_url: '', model: '', api_key_env: '', thinking: '' }

// 管理和 CLI 共用的模型配置，测试连接也复用 sec config ai test 的逻辑。
export default function AISettings() {
  const load = useCallback((signal: AbortSignal) => aiSettings(signal), [])
  const resource = useResource(load)
  // 修改接口返回修改后的全部配置，直接显示，不重新加载；刷新时清掉。
  const [changed, setChanged] = useState<Settings | null>(null)
  const data = changed ?? resource.data
  const [saving, setSaving] = useState(false)
  const [probe, setProbe] = useState<{ name: string; result?: AIProbeResult; error?: string } | null>(null)
  const probeRequest = useRef<AbortController | null>(null)
  const testing = probe !== null && !probe.result && probe.error === undefined
  const busy = saving || testing
  useEffect(() => () => probeRequest.current?.abort(), [])
  const [error, setError] = useState('')
  // editing 是正在编辑的配置名，'' 表示新增，null 表示没有打开表单。
  const [editing, setEditing] = useState<string | null>(null)
  const [form, setForm] = useState<AIProfileInput>(emptyForm)
  const [confirmEnable, setConfirmEnable] = useState(false)
  const [confirmRemove, setConfirmRemove] = useState('')
  // keyFor 是正在输入 Key 的配置名；confirmUnset 是等待确认清除 Key 的配置名。
  const [keyFor, setKeyFor] = useState('')
  const [keyValue, setKeyValue] = useState('')
  const [confirmUnset, setConfirmUnset] = useState('')

  const apply = async (change: AIChange) => {
    setSaving(true); setError(''); setProbe(null)
    try {
      setChanged(await changeAI(change))
      return true
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      return false
    } finally { setSaving(false) }
  }
  // 同一时间只打开一个表单或确认。输入的 Key 不留在页面状态里：关掉时清空。
  const closeAll = () => { setEditing(null); setConfirmEnable(false); setConfirmRemove(''); setKeyFor(''); setKeyValue(''); setConfirmUnset('') }
  const cancelProbe = () => {
    probeRequest.current?.abort()
    probeRequest.current = null
    setProbe(null)
  }
  const testProfile = async (name: string) => {
    cancelProbe(); closeAll(); setError('')
    const controller = new AbortController()
    probeRequest.current = controller
    setProbe({ name })
    try {
      const result = await testAIProfile(name, controller.signal)
      if (!controller.signal.aborted) setProbe({ name, result })
    } catch (e) {
      if (!controller.signal.aborted) setProbe({ name, error: e instanceof Error ? e.message : String(e) })
    } finally {
      if (probeRequest.current === controller) probeRequest.current = null
    }
  }
  // 刷新时关掉表单和确认：它们基于刷新前的数据，留着可能覆盖命令行刚做的修改。
  const reload = () => {
    cancelProbe(); setChanged(null); setError(''); closeAll()
    resource.reload()
  }
  const openForm = (name: string) => {
    const view = data?.profiles.find(p => p.name === name)
    closeAll()
    setForm(view && data ? profileForm(view, data.providers) : emptyForm)
    setEditing(name); setError('')
  }
  const saveKey = async (e: FormEvent) => {
    e.preventDefault()
    if (await apply({ kind: 'setKey', name: keyFor, key: keyValue })) closeAll()
  }
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const profile = trimProfile(form)
    const ok = await apply(editing ? { kind: 'edit', name: editing, profile } : { kind: 'add', profile })
    if (ok) setEditing(null)
  }

  const groups = privacyGroups(data?.profiles ?? [])
  return <section className="page-panel settings-panel" aria-labelledby="ai-settings-title">
    <h2 id="ai-settings-title">AI 模型</h2>
    <p className="data-note">模型配置和命令行共用 ~/.sec/config.json 的 ai 段，在这里或用 sec config ai 修改都可以。Key 加密保存在 config.json，加密密钥在 ~/.sec/secret.key。</p>
    <ResourceState {...resource} retry={reload} />
    {data && <>
      {data.invalid && <p className="error" role="alert">读取模型配置有问题，请手动修正后刷新：{data.invalid}</p>}
      <div className="setting-row"><div><span>AI 功能</span><p className="data-note">默认关闭，启用后才会调用模型。</p></div>
        {data.enabled
          ? <button disabled={busy} onClick={() => void apply({ kind: 'enabled', enabled: false })}>关闭</button>
          : <button disabled={busy || data.profiles.length === 0} onClick={() => { closeAll(); setConfirmEnable(true) }}>启用</button>}
      </div>
      {confirmEnable && !data.enabled && <div className="ai-confirm" role="alertdialog" aria-labelledby="ai-enable-title">
        <p id="ai-enable-title">使用 AI 功能时，查询的证券、自选股和数据结果会发送给所选配置的厂商，按厂商的隐私政策处理。</p>
        {groups.remote.length > 0 && <p className="data-note">会发送到外部厂商的配置：{groups.remote.join('、')}</p>}
        {groups.local.length > 0 && <p className="data-note">本机模型，数据不出本机：{groups.local.join('、')}</p>}
        <p className="data-note">回答只解释数据，不构成投资建议。可以随时在这里关闭。</p>
        <div className="version-actions">
          <button className="primary-button" disabled={busy} onClick={async () => { if (await apply({ kind: 'enabled', enabled: true })) setConfirmEnable(false) }}>确认启用</button>
          <button onClick={() => setConfirmEnable(false)}>取消</button>
        </div>
      </div>}
      {data.profiles.length === 0
        ? <p className="data-note">还没有模型配置，点「新增配置」添加。</p>
        : <div className="table-scroll"><table className="data-table">
          <thead><tr><th>配置</th><th>厂商</th><th>模型</th><th>地址</th><th>思考</th><th>Key</th><th>操作</th></tr></thead>
          <tbody>{data.profiles.map(p => <tr key={p.name}>
            <td>{p.name}{p.default && <span className="data-note">（默认）</span>}</td>
            <td>{p.provider_name}{p.local && <span className="data-note">（本机）</span>}</td>
            <td>{p.model}</td><td>{p.base_url}</td><td>{thinkingLabel(p)}</td><td>{p.key}</td>
            <td className="ai-actions">{confirmRemove === p.name
              ? <><button disabled={busy} onClick={async () => { if (await apply({ kind: 'remove', name: p.name })) setConfirmRemove('') }}>确认删除</button><button onClick={() => setConfirmRemove('')}>取消</button></>
              : confirmUnset === p.name
                ? <><button disabled={busy} onClick={async () => { if (await apply({ kind: 'unsetKey', name: p.name })) setConfirmUnset('') }}>确认清除 Key</button><button onClick={() => setConfirmUnset('')}>取消</button></>
                : <>
                  <button disabled={busy} onClick={() => openForm(p.name)}>编辑</button>
                  <button disabled={busy} onClick={() => { closeAll(); setKeyFor(p.name); setError('') }}>设置 Key</button>
                  <button disabled={busy} onClick={() => void testProfile(p.name)}>{testing && probe?.name === p.name ? '测试中…' : '测试连接'}</button>
                  {p.key_stored && <button disabled={busy} onClick={() => { closeAll(); setConfirmUnset(p.name) }}>清除 Key</button>}
                  {!p.default && <button disabled={busy} onClick={() => void apply({ kind: 'default', name: p.name })}>设为默认</button>}
                  {removable(p, data.profiles.length) && <button disabled={busy} onClick={() => { closeAll(); setConfirmRemove(p.name) }}>删除</button>}
                </>}</td>
          </tr>)}</tbody>
        </table></div>}
      {data.profiles.length > 1 && <p className="data-note">默认配置要先把别的配置设为默认才能删除。</p>}
      {data.profiles.length > 0 && <p className="data-note">测试连接会发送两条固定的测试消息，检查连接和工具调用，不含你的查询或自选股数据；AI 功能关闭时也可以测试。</p>}
      {probe && <div className="ai-confirm" aria-label={`测试模型配置 ${probe.name}`} aria-busy={testing}>
        <h3>测试模型配置 {probe.name}</h3>
        {testing && <p className="data-note" role="status">正在测试连接和工具调用，每个请求最长等待 2 分钟…</p>}
        {probe.result && <div role="status">
          <p className="data-note">厂商：{probe.result.provider}；协议：{probe.result.api}；模型：{probe.result.model}</p>
          <p className="data-note">地址：{probe.result.base_url}；Key：{probe.result.key}</p>
          {probeLines(probe.result).map((line, i) => <p key={i} className={line.startsWith('连接失败：') ? 'error' : 'data-note'}>{line}</p>)}
        </div>}
        {probe.error !== undefined && <p className="error" role="alert">测试请求失败：{probe.error}</p>}
        <button onClick={cancelProbe}>{testing ? '取消测试' : '关闭结果'}</button>
      </div>}
      {keyFor && <form className="ai-form" onSubmit={saveKey} aria-label={`设置 ${keyFor} 的 Key`}>
        <fieldset className="settings-group"><legend>设置 {keyFor} 的 Key</legend>
          <div className="setting-row"><div><label htmlFor="ai-key">API Key</label><p className="data-note">加密保存，之后只显示末 4 位，不会再显示完整的 Key。保存后不再从 Key 环境变量读取。本机模型一般不需要 Key，不用设置。</p></div>
            <input id="ai-key" type="password" required autoComplete="off" spellCheck={false} value={keyValue} onChange={e => setKeyValue(e.target.value)} /></div>
        </fieldset>
        <div className="settings-actions"><button className="primary-button" type="submit" disabled={busy}>{busy ? '保存中…' : '保存 Key'}</button><button type="button" disabled={busy} onClick={() => { closeAll(); setError('') }}>取消</button></div>
      </form>}
      {editing !== null
        ? <ProfileForm form={form} adding={editing === ''} name={editing} providers={data.providers} thinkingOffProviders={data.thinking_off_providers} busy={busy} onChange={setForm} onSubmit={submit} onCancel={() => { closeAll(); setError('') }} />
        : !keyFor && <div className="version-actions"><button disabled={busy || !!data.invalid} onClick={() => openForm('')}>新增配置</button></div>}
      {data.notice && <p className="data-note" role="status">{data.notice}</p>}
      {error && <p className="error" role="alert">{error}</p>}
      {groups.remoteNames.length > 0 && <p className="data-note">{groups.remoteNames.join('、')} 不是本机模型：使用 AI 功能时，查询的证券、自选股和数据结果会发送给模型厂商。本机模型（如 Ollama）不出本机。</p>}
      <p className="data-note">Key 只显示来源和末 4 位。从访达或开始菜单打开的 Desktop 读不到终端里设置的环境变量，用环境变量提供 Key 时这里可能显示「未设置」，可以改用「设置 Key」加密保存。</p>
    </>}
    {/* 刷新按钮放在数据外面：重新加载时数据先清空，按钮不随之消失。 */}
    <div className="version-actions"><button disabled={resource.loading || busy} onClick={reload}>刷新</button></div>
  </section>
}

function ProfileForm({ form, adding, name, providers, thinkingOffProviders, busy, onChange, onSubmit, onCancel }: {
  form: AIProfileInput
  adding: boolean
  name: string
  providers: AIProvider[]
  thinkingOffProviders: string[]
  busy: boolean
  onChange: (form: AIProfileInput) => void
  onSubmit: (e: FormEvent) => void
  onCancel: () => void
}) {
  const provider = providers.find(p => p.id === form.provider)
  const custom = provider?.api === ''
  const set = (field: keyof AIProfileInput) => (e: { target: { value: string } }) => onChange({ ...form, [field]: e.target.value })
  const thinking = thinkingSwitch(form, providers, thinkingOffProviders)
  const supportedNames = providers.filter(p => thinkingOffProviders.includes(p.id)).map(p => p.name).join('、')
  return <form className="ai-form" onSubmit={onSubmit} aria-label={adding ? '新增模型配置' : `编辑模型配置 ${name}`}>
    <fieldset className="settings-group"><legend>{adding ? '新增模型配置' : `编辑 ${name}`}</legend>
      {adding && <div className="setting-row"><div><label htmlFor="ai-name">配置名</label><p className="data-note">小写字母、数字和 -，以字母或数字开头，最长 32 个字符，如 ds。</p></div>
        <input id="ai-name" required pattern="[a-z0-9][a-z0-9\-]{0,31}" value={form.name ?? ''} onChange={set('name')} /></div>}
      <div className="setting-row"><div><label htmlFor="ai-provider">厂商</label><p className="data-note">其他厂商或自建网关选「自定义」，并填写协议和地址。换厂商时协议和地址回到新厂商的默认值。</p></div>
        <select id="ai-provider" value={form.provider} onChange={e => onChange(withProvider(form, e.target.value))}>{providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select></div>
      <div className="setting-row"><div><label htmlFor="ai-model">模型</label></div>
        <input id="ai-model" required placeholder="deepseek-chat" value={form.model} onChange={set('model')} /></div>
      <div className="setting-row"><div><label htmlFor="ai-api">接口协议</label><p className="data-note">{custom ? '自定义厂商必须选择。' : `留空用厂商默认的 ${provider?.api ?? ''}；换协议时要填对应的地址。`}</p></div>
        <select id="ai-api" required={custom} value={form.api} onChange={set('api')}><option value="">{custom ? '请选择' : '默认'}</option><option value="openai">openai</option><option value="anthropic">anthropic</option></select></div>
      <div className="setting-row"><div><label htmlFor="ai-base-url">接口地址</label><p className="data-note">{custom ? '自定义厂商必须填写。' : '留空用官方地址。'}不要在地址里放 Key。</p></div>
        <input id="ai-base-url" type="url" required={custom} placeholder={provider?.base_url || 'https://llm.example.com/v1'} value={form.base_url} onChange={set('base_url')} /></div>
      <div className="setting-row"><div><label htmlFor="ai-key-env">Key 环境变量</label><p className="data-note">可选，填环境变量名而不是 Key 本身。{provider?.key_env ? `留空时读取 ${provider.key_env}（用官方地址时）。` : ''}</p></div>
        <input id="ai-key-env" placeholder={provider?.key_env || 'MY_API_KEY'} value={form.api_key_env} onChange={set('api_key_env')} /></div>
      <div className="setting-row"><div><label htmlFor="ai-thinking">关闭思考</label><p className="data-note">{thinking.disabled || thinking.conflict
        ? `目前只支持 ${supportedNames} 官方地址的 OpenAI 兼容接口，当前的厂商、协议或地址不能关闭。`
        : '思考模型的推理也计入输出上限，长公告摘要可能用完上限、没有正文；关闭后更快。保存后可以「测试连接」，模型忽略了这个设置时会提示。'}</p>
        {thinking.conflict && <p className="error" role="alert">这个配置关闭了思考，但当前的厂商、协议或地址不能关闭，请取消勾选后再保存。</p>}</div>
        <input id="ai-thinking" type="checkbox" disabled={thinking.disabled} checked={thinking.checked} onChange={e => onChange({ ...form, thinking: e.target.checked ? 'off' : '' })} /></div>
    </fieldset>
    <div className="settings-actions"><button className="primary-button" type="submit" disabled={busy}>{busy ? '保存中…' : '保存'}</button><button type="button" onClick={onCancel}>取消</button></div>
  </form>
}
