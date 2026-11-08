import { useCallback } from 'react'
import { aiSettings } from '../api'
import { useResource } from '../hooks/useResource'
import ResourceState from './ResourceState'

// 只读显示和 CLI 共用的模型配置；新增、编辑和保存 Key 暂时用 sec config ai。
export default function AISettings() {
  const load = useCallback((signal: AbortSignal) => aiSettings(signal), [])
  const resource = useResource(load)
  const data = resource.data
  const remote = data?.profiles.filter(p => !p.local) ?? []
  return <section className="page-panel settings-panel" aria-labelledby="ai-settings-title">
    <h2 id="ai-settings-title">AI 模型</h2>
    <p className="data-note">模型配置和命令行共用 ~/.sec/config.json 的 ai 段，目前请用 sec config ai add、set-key、use、enable 管理，修改后在这里刷新查看。</p>
    <ResourceState {...resource} retry={resource.reload} />
    {data && <>
      {data.invalid && <p className="error" role="alert">读取模型配置有问题，请手动修正后刷新：{data.invalid}</p>}
      <div className="setting-row"><div><span>AI 功能</span><p className="data-note">默认关闭，用 sec config ai enable 启用后才会调用模型。</p></div><span>{data.enabled ? '已启用' : '未启用'}</span></div>
      {data.profiles.length === 0
        ? <p className="data-note">还没有模型配置。添加：sec config ai add ds --provider deepseek --model deepseek-chat</p>
        : <div className="table-scroll"><table className="data-table">
          <thead><tr><th>配置</th><th>厂商</th><th>模型</th><th>地址</th><th>Key</th></tr></thead>
          <tbody>{data.profiles.map(p => <tr key={p.name}>
            <td>{p.name}{p.default && <span className="data-note">（默认）</span>}</td>
            <td>{p.provider_name}{p.local && <span className="data-note">（本机）</span>}</td>
            <td>{p.model}</td><td>{p.base_url}</td><td>{p.key}</td>
          </tr>)}</tbody>
        </table></div>}
      {remote.length > 0 && <p className="data-note">{remote.map(p => p.name).join('、')} 不是本机模型：使用 AI 功能时，查询的证券、自选股和数据结果会发送给模型厂商。本机模型（如 Ollama）不出本机。</p>}
      <p className="data-note">Key 只显示来源和末 4 位。从访达或开始菜单打开的 Desktop 读不到终端里设置的环境变量，用环境变量提供 Key 时这里可能显示「未设置」，可以改用 sec config ai set-key 加密保存。</p>
    </>}
    {/* 刷新按钮放在数据外面：重新加载时数据先清空，按钮不随之消失。 */}
    <div className="version-actions"><button disabled={resource.loading} onClick={resource.reload}>刷新</button></div>
  </section>
}
