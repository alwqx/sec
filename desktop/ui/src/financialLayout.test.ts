import assert from 'node:assert/strict'
import test from 'node:test'
import { buildSankey, chartsFor, niceTicks, sankeySteps, sankeyUnavailable, waterfallRows } from './financialLayout.ts'
import type { FinancialBridge, FinancialCharts } from './types.ts'

const general: FinancialBridge = {
  start: { label: '营业总收入', value: 100 },
  steps: [
    { items: [{ label: '营业成本', value: -60 }], total: { label: '毛利', value: 40 } },
    { items: [{ label: '销售费用', value: -10 }, { label: '投资收益', value: 5 }], total: { label: '营业利润', value: 35 } },
  ],
}

test('buildSankey 每个节点的流入和流出都等于节点值', () => {
  const layout = buildSankey(general, 300)
  assert.ok(typeof layout !== 'string')
  for (const node of layout.nodes) {
    const outflow = layout.links.filter(link => link.source === node).reduce((sum, link) => sum + link.value, 0)
    const inflow = layout.links.filter(link => link.target === node).reduce((sum, link) => sum + link.value, 0)
    if (node.role === 'main' && node.col > 0) assert.equal(inflow, node.value, node.label)
    if (node.role === 'main' && node.col < layout.columns - 1) assert.equal(outflow, node.value, node.label)
  }
  assert.equal(layout.columns, 3)
  const gain = layout.nodes.find(node => node.label === '投资收益')!
  assert.equal(gain.col, 1, '收益从上一列流入')
  assert.equal(layout.nodes.find(node => node.label === '销售费用')!.col, 2, '支出流向下一列')
})

test('buildSankey 节点不超出高度，链接在节点内依次排列', () => {
  const layout = buildSankey(general, 300)
  assert.ok(typeof layout !== 'string')
  for (const node of layout.nodes) assert.ok(node.y + node.h <= 300 + 1e-9, node.label)
  const out = layout.links.filter(link => link.source.label === '毛利')
  assert.equal(out[0].target.label, '营业利润', '主干在最上面')
  assert.equal(out[1].sy, out[0].sy + out[0].w)
})

test('buildSankey 没有起点时第一步的收入汇成合计，负项从正项合计里扣掉', () => {
  const bank: FinancialBridge = {
    start: null,
    steps: [
      { items: [{ label: '利息净收入', value: 80 }, { label: '公允价值变动损失', value: -5 }, { label: '手续费', value: 25 }], total: { label: '营业收入', value: 100 } },
      { items: [{ label: '业务及管理费', value: -30 }], total: { label: '营业利润', value: 70 } },
    ],
  }
  const layout = buildSankey(bank, 300)
  assert.ok(typeof layout !== 'string')
  const merged = layout.nodes.find(node => node.label === '正项收入合计')!
  assert.equal(merged.value, 105)
  assert.equal(layout.nodes.find(node => node.label === '公允价值变动损失')!.role, 'cost')
  assert.equal(layout.nodes.find(node => node.label === '营业收入')!.value, 100)

  const plain = buildSankey({ start: null, steps: [{ items: [{ label: '利息', value: 60 }, { label: '手续费', value: 40 }], total: { label: '营业收入', value: 100 } }] }, 300)
  assert.ok(typeof plain !== 'string')
  assert.equal(plain.nodes.filter(node => node.role === 'main').length, 1, '没有负项时不需要中间合计')
})

test('buildSankey 亏损或支出超过收入时不画', () => {
  const loss: FinancialBridge = { start: { label: '营业总收入', value: 100 }, steps: [{ items: [{ label: '营业成本', value: -120 }], total: { label: '毛利', value: -20 } }] }
  assert.equal(buildSankey(loss, 300), sankeyUnavailable)
  const rescued: FinancialBridge = { start: { label: '毛利', value: 10 }, steps: [{ items: [{ label: '费用', value: -15 }, { label: '投资收益', value: 20 }], total: { label: '营业利润', value: 15 } }] }
  assert.equal(buildSankey(rescued, 300), sankeyUnavailable, '支出超过上一个合计，即使后面有收益补回也没法画')
})

test('waterfallRows 合计从 0 画起，变化接在上一行之后', () => {
  const rows = waterfallRows(general)
  assert.deepEqual(rows.map(row => [row.label, row.kind, row.from, row.to]), [
    ['营业总收入', 'total', 0, 100], ['营业成本', 'down', 100, 40], ['毛利', 'total', 0, 40],
    ['销售费用', 'down', 40, 30], ['投资收益', 'up', 30, 35], ['营业利润', 'total', 0, 35],
  ])
  const bank = waterfallRows({ start: null, steps: [{ items: [{ label: '利息', value: 60 }], total: { label: '营业收入', value: 60 } }] })
  assert.deepEqual(bank[0], { label: '利息', kind: 'up', from: 0, to: 60, value: 60 })
})

test('sankeySteps 把利润总额并入下一步，加总不变', () => {
  const bridge: FinancialBridge = {
    start: { label: '营业利润', value: 100 },
    steps: [
      { items: [{ label: '营业外支出', value: -2 }], total: { label: '利润总额', value: 98 } },
      { items: [{ label: '所得税', value: -18 }], total: { label: '净利润', value: 80 } },
    ],
  }
  const merged = sankeySteps(bridge)
  assert.equal(merged.steps.length, 1)
  assert.deepEqual(merged.steps[0].items.map(item => item.label), ['营业外支出', '所得税'])
  assert.equal(merged.steps[0].total.label, '净利润')
})

test('niceTicks 覆盖最小值和最大值', () => {
  for (const [low, high] of [[0, 91.2], [0, 1720.54e8], [-1.2, 10.4], [-3249e8, 4514e8], [0, 0.3]]) {
    const ticks = niceTicks(low, high)
    assert.ok(ticks[0] <= low && ticks[ticks.length - 1] >= high, `${low} ${high} ${ticks}`)
    assert.ok(ticks.length <= 7, `${ticks}`)
  }
  assert.deepEqual(niceTicks(0, 91.2), [0, 25, 50, 75, 100])
})

test('chartsFor 不在另一种频率下显示旧数据', () => {
  const annual = { code: 'SH600519', company_type: 'general', quarterly: false, periods: [] } as FinancialCharts
  const quarterly = { ...annual, quarterly: true }
  assert.equal(chartsFor(quarterly, annual, true), quarterly, '有新结果用新结果')
  assert.equal(chartsFor(null, annual, false), annual, '刷新同一频率时保留旧图')
  assert.equal(chartsFor(null, annual, true), null, '切到单季失败时不能显示年度数据')
  assert.equal(chartsFor(null, null, false), null)
})
