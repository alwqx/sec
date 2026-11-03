import assert from 'node:assert/strict'
import test from 'node:test'
import { money } from './format.ts'

test('money 缺失值显示破折号，0 是真实值', () => {
  assert.equal(money(null), '—')
  assert.equal(money(undefined), '—')
  assert.equal(money(0), '0.00 元')
})

test('money 按数量级选择单位', () => {
  assert.equal(money(9999), '9999.00 元')
  assert.equal(money(1e4), '1.00 万元')
  assert.equal(money(1e8), '1.00 亿元')
  assert.equal(money(1.5e12), '1.50 万亿元')
})

test('money 亏损按绝对值选择单位', () => {
  assert.equal(money(-2.5e8), '-2.50 亿元')
})
