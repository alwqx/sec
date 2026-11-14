import type { AIModels } from '../hooks/useAIModels'

// SummaryModelPicker 是摘要列表上方的模型配置下拉框、刷新按钮和隐私说明，公告和申报列表共用。
// sends 说明点击摘要会把什么发给模型厂商，如「公告 PDF 提取的文本」；extra 是附加说明。
export default function SummaryModelPicker({ models, sends, extra }: { models: AIModels; sends: string; extra: string }) {
  const { settings, loading, reload, profile, unavailable, select } = models
  return <>
    <div className="section-toolbar"><label>摘要模型配置<select aria-label="摘要模型配置" value={profile?.name ?? ''} disabled={loading || !profile} onChange={event => select(event.target.value)}>
      {!profile && <option value="">暂无配置</option>}
      {settings?.profiles.map(item => <option key={item.name} value={item.name}>{item.name}{item.default ? '（默认）' : ''} · {item.model}</option>)}
    </select></label><button disabled={loading} onClick={reload}>刷新模型配置</button></div>
    {unavailable && <p className="data-note" role="status">{unavailable}</p>}
    {profile && <p className="data-note">{profile.local ? `本机模型，${sends}不出本机。` : `点击摘要会将${sends}发送给 ${profile.provider_name}（${profile.name}）。`} 临时切换只对当前页面有效，不修改默认配置。{extra}</p>}
  </>
}
