import assert from 'node:assert/strict'
import test from 'node:test'
import { securityFromCode } from './security.ts'

test('securityFromCode 由代码推断交易所', () => {
  for (const [code, exCode, exchange] of [
    ['600036', 'SH600036', 'sse'], ['688047', 'SH688047', 'sse'], ['900901', 'SH900901', 'sse'],
    ['000001', 'SZ000001', 'szse'], ['300750', 'SZ300750', 'szse'], ['200002', 'SZ200002', 'szse'],
    ['430047', 'BJ430047', 'bse'], ['830799', 'BJ830799', 'bse'], ['920045', 'BJ920045', 'bse'],
  ]) {
    const result = securityFromCode(code, '名称')
    assert.equal(result?.ex_code, exCode, code)
    assert.equal(result?.exchange, exchange, code)
  }
})

test('securityFromCode 拒绝不是 6 位数字的代码', () => {
  for (const code of ['', '60003', '6000361', 'SH6000', '00700']) assert.equal(securityFromCode(code, '名称'), null, code)
})
