import assert from 'node:assert/strict'
import test from 'node:test'
import { money, tradeAmount, tradeShares, yi } from './format.ts'

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

test('yi 固定按亿显示，缺失值显示破折号', () => {
  assert.equal(yi(null), '—')
  assert.equal(yi(13210000000), '132.10')
  assert.equal(yi(132175000000), '1,321.75')
  assert.equal(yi(-22759000000), '-227.59')
})

test('tradeAmount、tradeShares 基金净值没有成交时显示破折号，0 是真实值', () => {
  assert.equal(tradeAmount(null), '—')
  assert.equal(tradeShares(null), '—')
  assert.equal(tradeAmount(0), '0')
  assert.equal(tradeShares(0), '0 股')
  assert.equal(tradeAmount(3.97e9), '39.70 亿')
  assert.equal(tradeShares(92954343), '9295.43 万股')
})
