import { useEffect, useState } from 'react'
import VersionInfo from './VersionInfo'
import AISettings from './AISettings'
import type { WorkspaceSettings } from '../types'
import { settingsToSave } from '../workspace'

export default function Settings({ settings, saving, onSave }: {
  settings: WorkspaceSettings
  saving: boolean
  onSave: (settings: WorkspaceSettings) => Promise<boolean>
}) {
  const [draft, setDraft] = useState(settings)
  const [saved, setSaved] = useState(false)
  // 邮箱和 CLI 共用：只在用户编辑过时保存；没编辑过时跟随后端返回的最新值（可能被 sec config 改过），
  // 不把页面打开时的旧值当成未保存的改动。
  const [emailEdited, setEmailEdited] = useState(false)
  useEffect(() => {
    if (!emailEdited) setDraft(current => ({ ...current, sec_edgar_email: settings.sec_edgar_email }))
  }, [settings.sec_edgar_email, emailEdited])
  const changed = draft.download_directory !== settings.download_directory || draft.theme !== settings.theme || draft.refresh_seconds !== settings.refresh_seconds || draft.sec_edgar_email !== settings.sec_edgar_email
  return <div className="settings-page"><section className="page-panel settings-panel">
    <h2>工作台偏好</h2>
    <p className="data-note">自选证券与 CLI 共用本机列表；偏好修改后点击保存，重新打开应用后继续使用。</p>
    <form onSubmit={async e => {
      e.preventDefault()
      const ok = await onSave(settingsToSave(draft, emailEdited))
      if (ok) setEmailEdited(false)
      setSaved(ok)
    }}>
      <fieldset className="settings-group"><legend>行情与外观</legend>
      <div className="setting-row"><div><label htmlFor="refresh-interval">行情刷新间隔</label><p className="data-note">仅在查看个股行情时刷新</p></div><select id="refresh-interval" value={draft.refresh_seconds} onChange={e => { setDraft({ ...draft, refresh_seconds: Number(e.target.value) }); setSaved(false) }}>{[5, 15, 30, 60].map(n => <option value={n} key={n}>{n} 秒</option>)}</select></div>
      <div className="setting-row"><div><label htmlFor="theme">外观</label><p className="data-note">选择深色、浅色或跟随系统</p></div><select id="theme" value={draft.theme} onChange={e => { setDraft({ ...draft, theme: e.target.value as WorkspaceSettings['theme'] }); setSaved(false) }}><option value="dark">深色</option><option value="light">浅色</option><option value="system">跟随系统</option></select></div>
      </fieldset><fieldset className="settings-group"><legend>下载</legend>
      <div className="setting-row"><div><label htmlFor="download-directory">下载目录</label><p className="data-note">财报导出和年报 PDF 共用此目录。留空使用 ~/Downloads；支持绝对路径或 ~/ 开头，下载时自动创建目录。</p></div><input id="download-directory" type="text" placeholder="~/Downloads" value={draft.download_directory ?? ''} onChange={e => { setDraft({ ...draft, download_directory: e.target.value }); setSaved(false) }} /></div>
      </fieldset><fieldset className="settings-group"><legend>数据源</legend>
      <div className="setting-row"><div><label htmlFor="sec-email">SEC 联系邮箱</label><p className="data-note">访问美国 SEC EDGAR（美股申报）时写在请求头里，SEC 要求提供，否则拒绝访问。和命令行共用 ~/.sec/config.json，也可以用 sec config set sec-edgar-email 修改。</p></div><input id="sec-email" type="email" required placeholder="name@example.com" value={draft.sec_edgar_email ?? ''} onChange={e => { setDraft({ ...draft, sec_edgar_email: e.target.value.trim() }); setEmailEdited(true); setSaved(false) }} /></div>
      </fieldset>
      <div className="settings-actions"><button className="primary-button" type="submit" disabled={saving || !changed}>{saving ? '保存中…' : '保存设置'}</button><span role="status">{saved && !changed ? '设置已保存' : ''}</span></div>
    </form>
    </section><AISettings /><section className="page-panel settings-about"><h3>关于 SEC Desktop</h3><p className="data-note">证券研究工作台 · 支持 A 股、港股和美股行情。公司资料和财报支持 A 股和美股（SEC EDGAR），分红目前只有 A 股；公告支持 A 股（巨潮资讯）和美股（SEC 申报）。</p><VersionInfo /></section>
  </div>
}
