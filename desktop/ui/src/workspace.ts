import type { WorkspaceSettings } from './types'

// settingsToSave 决定保存时带不带 SEC 联系邮箱：只在用户编辑过邮箱输入框时发送。
// 邮箱保存在和 CLI 共用的 ~/.sec/config.json，打开 Desktop 后可能被 sec config 改过；页面上的值可能是旧的，
// 不编辑就发送会把它改回去。后端收到空邮箱时不修改。按是否编辑过判断，不和读到的值比较：读到的值本身也可能过期。
export function settingsToSave(settings: WorkspaceSettings, emailEdited: boolean): WorkspaceSettings {
  if (emailEdited) return settings
  const { sec_edgar_email: _, ...rest } = settings
  return rest
}
