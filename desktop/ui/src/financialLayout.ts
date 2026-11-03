import type { FinancialBridge, FinancialCharts, FinancialItem } from './types'

// 桑基图的节点：main 是主干上的合计，cost 是流出的支出，gain 是流入的收入或收益。
export interface SankeyNode { id: string; label: string; value: number; col: number; role: 'main' | 'cost' | 'gain'; y: number; h: number }
export interface SankeyLink { source: SankeyNode; target: SankeyNode; value: number; role: SankeyNode['role']; sy: number; ty: number; w: number }
export interface SankeyLayout { nodes: SankeyNode[]; links: SankeyLink[]; columns: number }

export const sankeyUnavailable = '某一步的支出超过收入，或利润为负，桑基图无法表示，请看瀑布图。'

// buildSankey 把首尾相接的合计排成桑基图。每一列的顺序是：主干合计、上一步流出的支出、下一步流入的收益。
// 每一步要求上一个合计扣掉支出后不为负，否则没法用流量表示。
export function buildSankey(bridge: FinancialBridge, height: number, gap = 8, minSpace = 16): SankeyLayout | string {
  const nodes: SankeyNode[] = []
  const links: Omit<SankeyLink, 'sy' | 'ty' | 'w'>[] = []
  const add = (label: string, value: number, col: number, role: SankeyNode['role']) => {
    const node = { id: `${nodes.length}`, label, value, col, role, y: 0, h: 0 }
    nodes.push(node)
    return node
  }
  let main: SankeyNode | null = bridge.start ? add(bridge.start.label, bridge.start.value, 0, 'main') : null
  if (main && main.value <= 0) return sankeyUnavailable
  let col = 0
  for (const step of bridge.steps) {
    let gains = step.items.filter(item => item.value > 0)
    const costs = step.items.filter(item => item.value < 0)
    const plus = gains.reduce((sum, item) => sum + item.value, 0)
    if (!main) {
      // 没有起点时，第一步的收入直接汇成合计；有负项时先汇总正项，再从中扣掉负项。
      const merged = costs.length ? add('正项收入合计', plus, col + 1, 'main') : add(step.total.label, step.total.value, col + 1, 'main')
      for (const item of gains) links.push({ source: add(item.label, item.value, col, 'gain'), target: merged, value: item.value, role: 'gain' })
      col += 1
      main = merged
      if (!costs.length) continue
      gains = []
    }
    const minus = costs.reduce((sum, item) => sum - item.value, 0)
    const through = main.value - minus
    const total = add(step.total.label, step.total.value, col + 1, 'main')
    if (main.value <= 0 || total.value <= 0 || through < -0.5) return sankeyUnavailable
    if (through > 0.5) links.push({ source: main, target: total, value: through, role: 'main' })
    for (const item of costs) links.push({ source: main, target: add(item.label, -item.value, col + 1, 'cost'), value: -item.value, role: 'cost' })
    for (const item of gains) links.push({ source: add(item.label, item.value, col, 'gain'), target: total, value: item.value, role: 'gain' })
    col += 1
    main = total
  }
  const columns = col + 1
  // 按最挤的一列定比例；很小的节点也留出一行文字的位置。
  let scale = Infinity
  for (let c = 0; c < columns; c++) {
    const inColumn = nodes.filter(node => node.col === c)
    const sum = inColumn.reduce((total, node) => total + node.value, 0)
    if (sum > 0) scale = Math.min(scale, (height - gap * (inColumn.length - 1) - minSpace * inColumn.length) / sum)
  }
  scale = Math.max(scale, 0)
  for (let c = 0; c < columns; c++) {
    let y = 0
    for (const node of nodes.filter(node => node.col === c)) {
      node.h = node.value * scale
      node.y = y
      y += Math.max(node.h, minSpace) + gap
    }
  }
  const out = new Map<string, number>(), into = new Map<string, number>()
  const placed = links.map(link => {
    const w = link.value * scale
    const sy = link.source.y + (out.get(link.source.id) ?? 0)
    const ty = link.target.y + (into.get(link.target.id) ?? 0)
    out.set(link.source.id, (out.get(link.source.id) ?? 0) + w)
    into.set(link.target.id, (into.get(link.target.id) ?? 0) + w)
    return { ...link, sy, ty, w }
  })
  return { nodes, links: placed, columns }
}

// sankeySteps 把“利润总额”这一步并入下一步，桑基图少一列，每列的文字才放得下；瀑布图仍然逐项展示。
export function sankeySteps(bridge: FinancialBridge): FinancialBridge {
  const steps: FinancialBridge['steps'] = []
  for (const step of bridge.steps) {
    const previous = steps[steps.length - 1]
    if (previous?.total.label === '利润总额') steps[steps.length - 1] = { items: [...previous.items, ...step.items], total: step.total }
    else steps.push(step)
  }
  return { start: bridge.start, steps }
}

export interface WaterfallRow { label: string; kind: 'total' | 'up' | 'down'; from: number; to: number; value: number }

// waterfallRows 把合计和每一项变化排成瀑布图的行：合计从 0 画起，变化从上一行的终点画起。
export function waterfallRows(bridge: FinancialBridge): WaterfallRow[] {
  const rows: WaterfallRow[] = []
  let current = 0
  const total = (item: FinancialItem) => { rows.push({ label: item.label, kind: 'total', from: 0, to: item.value, value: item.value }); current = item.value }
  if (bridge.start) total(bridge.start)
  for (const step of bridge.steps) {
    for (const item of step.items) {
      rows.push({ label: item.label, kind: item.value >= 0 ? 'up' : 'down', from: current, to: current + item.value, value: item.value })
      current += item.value
    }
    total(step.total)
  }
  return rows
}

// niceTicks 返回覆盖 [low, high] 的整齐刻度，第一个不大于 low，最后一个不小于 high。
export function niceTicks(low: number, high: number) {
  const span = (high - low) || Math.abs(high) || 1
  const raw = span / 4, power = 10 ** Math.floor(Math.log10(raw))
  const unit = [1, 2, 2.5, 5, 10].map(n => n * power).find(n => n >= raw) ?? raw
  const start = Math.floor(low / unit) * unit, end = Math.ceil(high / unit) * unit
  const ticks: number[] = []
  for (let value = start; value <= end + unit * 1e-6; value += unit) ticks.push(Number(value.toPrecision(12)))
  return ticks
}

// chartsFor 选择要画的数据：有新结果用新结果，否则沿用上一次的结果，但只在频率相同的时候。
export function chartsFor(current: FinancialCharts | null, last: FinancialCharts | null, quarterly: boolean) {
  if (current) return current
  return last?.quarterly === quarterly ? last : null
}
