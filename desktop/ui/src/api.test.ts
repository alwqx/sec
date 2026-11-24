import assert from 'node:assert/strict'
import test from 'node:test'
import { APIError, canRetryError, errorStatus, usDividends, usFinancialCharts, usFinancials } from './api.ts'

test('request 失败时保留 HTTP 状态码并透传后端错误文案', async t => {
  const originalFetch = globalThis.fetch
  t.after(() => { globalThis.fetch = originalFetch })

  globalThis.fetch = async () => new Response(JSON.stringify({ error: 'SEC EDGAR 没有该公司的 XBRL 数据' }), {
    status: 404,
    headers: { 'Content-Type': 'application/json' },
  })
  await assert.rejects(
    () => usFinancials('$TEST', 'income', 'annual'),
    (err: unknown) => {
      assert.ok(err instanceof APIError)
      assert.equal(err.status, 404)
      assert.equal(err.message, 'SEC EDGAR 没有该公司的 XBRL 数据')
      assert.equal(errorStatus(err), 404)
      return true
    },
  )

  globalThis.fetch = async () => new Response('bad gateway', { status: 502 })
  await assert.rejects(
    () => usDividends('$TEST'),
    (err: unknown) => {
      assert.ok(err instanceof APIError)
      assert.equal(err.status, 502)
      assert.equal(err.message, '请求失败 (502)')
      assert.equal(errorStatus(err), 502)
      return true
    },
  )

  globalThis.fetch = async () => new Response(JSON.stringify({ error: '暂无财务报表' }), {
    status: 404,
    headers: { 'Content-Type': 'application/json' },
  })
  await assert.rejects(
    () => usFinancialCharts('$TEST', 'annual'),
    (err: unknown) => err instanceof APIError && err.status === 404,
  )
})

test('errorStatus 和 canRetryError 识别 404 并保留其余错误的重试能力', () => {
  assert.equal(errorStatus(new APIError('not found', 404)), 404)
  assert.equal(errorStatus(new Error('network error')), null)
  assert.equal(errorStatus('boom'), null)

  assert.equal(canRetryError(404), false, '404 表示没有数据，不显示重新加载按钮')
  for (const status of [400, 429, 500, 502, 504, null, undefined]) {
    assert.equal(canRetryError(status), true, `status=${String(status)} 仍允许重试`)
  }
})
