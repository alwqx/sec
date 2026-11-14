import assert from 'node:assert/strict'
import test from 'node:test'
import { filingRefLinker, summarizableFiling } from './aiSummary.ts'

test('只有 8-K 和 8-K/A 可以摘要', () => {
  assert.equal(summarizableFiling('8-K'), true)
  assert.equal(summarizableFiling('8-K/A'), true)
  for (const form of ['10-K', '6-K', '8-K12B', '']) assert.equal(summarizableFiling(form), false, form)
})

test('引用标签对应到各节原文地址，未知标签不给链接', () => {
  const result = { sources: [{ label: 'Item 2.02', url: 'https://www.sec.gov/a.htm' }, { label: 'Exhibit 99.1', url: 'https://www.sec.gov/ex991.htm' }] }
  assert.deepEqual(filingRefLinker(result)(['Item 2.02', 'Exhibit 99.1', 'Item 9.99']), [
    { label: 'Item 2.02', url: 'https://www.sec.gov/a.htm' },
    { label: 'Exhibit 99.1', url: 'https://www.sec.gov/ex991.htm' },
    { label: 'Item 9.99', url: undefined },
  ])
})
