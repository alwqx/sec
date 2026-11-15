import assert from 'node:assert/strict'
import test from 'node:test'
import { filingRefLinker, isEightK, reportDateLabel, riskFiling, summarizableFiling, summaryPrivacy } from './aiSummary.ts'

test('8-K（含修订版）、原始 10-K 和 10-KT 有摘要按钮', () => {
  for (const form of ['8-K', '8-K/A', '10-K', '10-KT']) assert.equal(summarizableFiling(form), true, form)
  for (const form of ['10-K/A', '10-KT/A', '10-Q', '6-K', '8-K12B', '20-F', '']) assert.equal(summarizableFiling(form), false, form)
})

test('只有原始 10-K 和 10-KT 有风险因素对比，修订版多半只补充第三部分', () => {
  for (const form of ['10-K', '10-KT']) assert.equal(riskFiling(form), true, form)
  for (const form of ['10-K/A', '8-K', '10-Q', '20-F', '']) assert.equal(riskFiling(form), false, form)
})

test('8-K 的报告期是事项日，10-K 是报告期', () => {
  assert.equal(isEightK('8-K/A'), true)
  assert.equal(reportDateLabel('8-K/A'), '事项日')
  assert.equal(reportDateLabel('10-K'), '报告期')
})

test('隐私说明按页面上的申报类型写', () => {
  assert.deepEqual(summaryPrivacy(['8-K', '10-Q']), { sends: '8-K 正文和 EX-99 附件的文本', extra: '8-K 摘要引用对应 Item 和附件。' })
  const both = summaryPrivacy(['10-K', '8-K/A'])
  assert.equal(both.sends, '8-K 正文和 EX-99 附件、10-K 风险因素和管理层讨论与分析的文本')
  assert.match(both.extra, /点「解读」才把变化发给模型/)
})

test('引用标签对应到各节原文地址，未知标签不给链接', () => {
  const result = { sources: [{ label: 'Item 2.02', url: 'https://www.sec.gov/a.htm' }, { label: 'Exhibit 99.1', url: 'https://www.sec.gov/ex991.htm' }] }
  assert.deepEqual(filingRefLinker(result)(['Item 2.02', 'Exhibit 99.1', 'Item 9.99']), [
    { label: 'Item 2.02', url: 'https://www.sec.gov/a.htm' },
    { label: 'Exhibit 99.1', url: 'https://www.sec.gov/ex991.htm' },
    { label: 'Item 9.99', url: undefined },
  ])
})
