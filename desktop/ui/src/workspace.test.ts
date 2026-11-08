import assert from 'node:assert/strict'
import test from 'node:test'
import type { WorkspaceSettings } from './types'
import { settingsToSave } from './workspace.ts'

const settings: WorkspaceSettings = { download_directory: '/tmp', refresh_seconds: 5, theme: 'dark', sidebar_hidden: true, sec_edgar_email: 'old@example.org' }

test('settingsToSave 没编辑过邮箱时不发送，避免用页面上的旧值覆盖 CLI 的修改', () => {
  const out = settingsToSave(settings, false)
  assert.equal(JSON.stringify(out).includes('sec_edgar_email'), false, '请求里没有这个字段')
  assert.equal(out.sidebar_hidden, true, '其他设置照常发送')
  assert.equal(out.theme, 'dark')
  assert.equal(settings.sec_edgar_email, 'old@example.org', '不修改传入的设置')
})

test('settingsToSave 编辑过邮箱时发送，改回原来的值也发送', () => {
  assert.equal(settingsToSave({ ...settings, sec_edgar_email: 'new@example.org' }, true).sec_edgar_email, 'new@example.org')
  assert.equal(settingsToSave(settings, true).sec_edgar_email, 'old@example.org')
})
