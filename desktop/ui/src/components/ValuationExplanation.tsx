import { useCallback, useEffect, useRef, useState } from 'react'
import { aiSettings, explainValuation } from '../api'
import { explanationChecks, explanationInput, explanationProfile, explanationUnavailable } from '../aiValuation'
import { useResource } from '../hooks/useResource'
import type { AIProfileView, ValuationExplanation as Explanation, ValuationResult } from '../types'

export default function ValuationExplanation({ code, params, data, loading, error }: {
  code: string
  params: Record<string, string>
  data: ValuationResult | null
  loading: boolean
  error: string
}) {
  const loadModels = useCallback((signal: AbortSignal) => aiSettings(signal), [])
  const models = useResource(loadModels)
  const [selected, setSelected] = useState('')
  const profile = explanationProfile(models.data, selected)
  const [run, setRun] = useState<{ source: ValuationResult; profile: AIProfileView; result?: Explanation; error?: string } | null>(null)
  const controller = useRef<AbortController | null>(null)
  // 证券、模型或已应用的估值参数变了时取消请求。旧响应不能覆盖新页面或新的配置选择。
  useEffect(() => {
    controller.current?.abort()
    controller.current = null
    setRun(null)
    return () => controller.current?.abort()
  }, [code, params, data, profile])
  const current = run?.source === data && run?.profile === profile ? run : null
  const busy = !!current && !current.result && current.error === undefined
  const unavailable = models.error ? `读取模型配置失败：${models.error}` : explanationUnavailable(models.data)
  const blocked = !!unavailable || !profile || !data || loading || !!error
  const cancel = () => { controller.current?.abort(); controller.current = null; setRun(null) }
  const generate = async () => {
    if (blocked || !data || !profile) return
    cancel()
    const request = new AbortController()
    controller.current = request
    const source = data
    setRun({ source, profile })
    try {
      const result = await explainValuation(explanationInput(code, profile.name, params), request.signal)
      if (!request.signal.aborted) setRun({ source, profile, result })
    } catch (e) {
      if (!request.signal.aborted) setRun({ source, profile, error: e instanceof Error ? e.message : String(e) })
    } finally {
      if (controller.current === request) controller.current = null
    }
  }
  const result = current?.result
  return <section className="valuation-explanation" aria-label="AI 估值解读">
    <div className="section-toolbar">
      <h3>AI 估值解读</h3>
      <label>模型配置<select aria-label="解读模型配置" value={profile?.name ?? ''} disabled={models.loading || !profile} onChange={event => setSelected(event.target.value)}>
        {!profile && <option value="">暂无配置</option>}
        {models.data?.profiles.map(item => <option key={item.name} value={item.name}>{item.name}{item.default ? '（默认）' : ''} · {item.model}</option>)}
      </select></label>
      <button disabled={models.loading || busy} onClick={models.reload}>刷新模型配置</button>
      <button className="primary-button" disabled={blocked || busy} onClick={() => void generate()}>{busy ? '解读中…' : result ? '重新解读' : '解读'}</button>
      {busy && <button onClick={cancel}>取消解读</button>}
    </div>
    {unavailable && <p className="data-note" role="status">{unavailable}</p>}
    {profile && <p className="data-note">{profile.local ? '本机模型，估值数据不出本机。' : `点击解读会将这只证券的估值数据和口径说明发送给 ${profile.provider_name}（${profile.name}）。`} 临时切换只对当前页面有效，不修改默认配置。</p>}
    <p className="data-note">解读时会按已应用的参数重新查询估值，以解读中的数据截止日期为准。</p>
    {!unavailable && (loading || error) && <p className="data-note">请先等待估值加载完成或修正估值参数，再生成解读。</p>}
    {busy && <p className="data-note" role="status">正在生成解读，可能需要几分钟，可以取消…</p>}
    {current?.error !== undefined && <p className="error" role="alert">{current.error}</p>}
    {result && !blocked && <div className="explanation-result" role="status">
      <p className="data-note">{result.name}（{result.ex_code}） · 数据截止 {result.as_of} · 模型 {result.model} · 配置 {result.profile}{result.cached ? ` · 缓存，生成于 ${new Date(result.created_at).toLocaleString('zh-CN')}` : ''}</p>
      <p className="explanation-text">{result.text}</p>
      {result.truncated && <p className="error">输出达到上限，解读不完整。</p>}
      {explanationChecks(result).map(line => <p key={line} className={result.check.unsourced.length || result.check.advice_terms.length ? 'error' : 'data-note'}>核对：{line}</p>)}
      <p className="data-note">{result.disclaimer}</p>
    </div>}
  </section>
}
