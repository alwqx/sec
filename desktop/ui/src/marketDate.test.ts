import assert from 'node:assert/strict'
import test from 'node:test'
import { frequencies, marketToday, monthsAgo, rangeLabel } from './marketDate.ts'

test('marketToday 是 YYYY-MM-DD', () => {
  assert.match(marketToday(), /^\d{4}-\d{2}-\d{2}$/)
})

test('monthsAgo 按自然月回溯', () => {
  assert.equal(monthsAgo(1, '2026-11-02'), '2026-10-02')
  assert.equal(monthsAgo(12, '2026-11-02'), '2025-11-02')
  assert.equal(monthsAgo(3, '2026-01-15'), '2025-10-15', '跨年')
})

test('monthsAgo 月末限制在目标月份的最后一天', () => {
  assert.equal(monthsAgo(1, '2026-03-31'), '2026-02-28')
  assert.equal(monthsAgo(1, '2024-03-31'), '2024-02-29', '闰年')
  assert.equal(monthsAgo(4, '2026-07-31'), '2026-03-31')
})

test('rangeLabel 不足一年按月，否则按年', () => {
  assert.equal(rangeLabel(6), '近 6 个月')
  assert.equal(rangeLabel(12), '近 1 年')
  assert.equal(rangeLabel(120), '近 10 年')
})

test('每个周期的快捷区间和默认区间都不超过前端声明的上限', () => {
  for (const item of Object.values(frequencies)) {
    assert.ok(item.months <= item.maxYears * 12, item.title)
    for (const months of item.ranges) assert.ok(months <= item.maxYears * 12, `${item.title} ${months}`)
  }
})
