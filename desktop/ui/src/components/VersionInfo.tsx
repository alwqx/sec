import { useCallback, useEffect, useRef, useState, type MouseEvent } from 'react'
import { checkUpdates, versionDetails } from '../api'
import { useResource } from '../hooks/useResource'
import type { UpdateDetails } from '../types'
import ResourceState from './ResourceState'

export default function VersionInfo() {
  const load = useCallback((signal: AbortSignal) => versionDetails(signal), [])
  const resource = useResource(load)
  const [update, setUpdate] = useState<UpdateDetails | null>(null)
  const [checking, setChecking] = useState(false)
  const [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  useEffect(() => () => controller.current?.abort(), [])
  const check = async () => {
    if (controller.current && !controller.current.signal.aborted) return
    const request = new AbortController()
    controller.current = request
    setChecking(true); setError(''); setUpdate(null)
    try { const data = await checkUpdates(request.signal); if (!request.signal.aborted) setUpdate(data) }
    catch (e) { if (!request.signal.aborted) setError(e instanceof Error ? e.message : String(e)) }
    finally { if (!request.signal.aborted) { setChecking(false); controller.current = null } }
  }
  const open = (event: MouseEvent<HTMLAnchorElement>, url: string) => {
    if (!window.secOpenRelease) return
    event.preventDefault()
    void window.secOpenRelease(url).catch(() => setError('打开发布页面失败，请在浏览器访问 https://github.com/alwqx/sec/releases/latest'))
  }
  const link = (url: string, label: string) => <a className="text-link" href={url} target="_blank" rel="noopener noreferrer" onClick={event => open(event, url)}>{label} ↗</a>
  return <section className="version-info" aria-label="版本与更新">
    <ResourceState {...resource} retry={resource.reload} />
    {resource.data && <dl className="version-details">
      <div><dt>版本</dt><dd>{resource.data.version || '开发构建（未注入版本号）'}</dd></div>
      <div><dt>构建时间</dt><dd>{resource.data.build_time || '未知'}</dd></div>
      <div><dt>提交号</dt><dd>{resource.data.git_commit || '未知'}</dd></div>
      <div><dt>平台</dt><dd>{resource.data.os} / {resource.data.arch}</dd></div>
    </dl>}
    <div className="version-actions"><button disabled={checking} onClick={() => void check()}>{checking ? '检查中…' : '检查更新'}</button>{link(resource.data?.release_url || 'https://github.com/alwqx/sec/releases/latest', '查看 Release')}</div>
    {checking && <p className="data-note" role="status">正在查询 GitHub 最新正式版本…</p>}
    {error && <p className="error" role="alert">{error}</p>}
    {update && <div role="status">
      <p>{({ available: '发现新版本', current: '当前已是最新正式版本', ahead: '当前版本高于最新正式版本', unknown: '当前为开发或无法比较的版本，请手动确认' })[update.status]} · {update.latest_version}</p>
      <div className="version-actions">{link(update.release_url, '版本详情与下载')}{update.asset && link(update.asset.browser_download_url, `下载 Desktop 安装包（${(update.asset.size / 1024 / 1024).toFixed(1)} MB）`)}</div>
      {!update.asset && <p className="data-note">最新版本未提供匹配当前平台的 Desktop 安装包，请在 Release 页面查看可用文件。</p>}
    </div>}
    <p className="data-note">仅在点击时检查更新；下载和安装通过系统浏览器进行，安装前请退出应用。</p>
  </section>
}
