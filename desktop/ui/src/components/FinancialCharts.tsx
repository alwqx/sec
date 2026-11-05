import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from 'react'
import { financialCharts, usFinancialCharts } from '../api'
import { useResource } from '../hooks/useResource'
import { buildSankey, chartsFor, niceTicks, sankeySteps, waterfallRows } from '../financialLayout'
import type { FinancialBridge, FinancialCharts as ChartData, FinancialMetrics, FinancialPeriod } from '../types'
import ResourceState from './ResourceState'

const yi = (value: number) => `${(value / 1e8).toLocaleString('zh-CN', { maximumFractionDigits: 2, minimumFractionDigits: 2 })} 亿`
const pct = (value: number) => `${value.toFixed(1)}%`
const times = (value: number) => `${value.toFixed(2)} 倍`
const series = (i: number) => `var(--series-${i + 1})`
const roleColor = { main: series(0), cost: series(1), gain: series(2) }

// periodLabel 优先用后端给的标注（美股按财年，比如 FY2026Q1），A 股按日期标注。
function periodLabel(period: FinancialPeriod, quarterly: boolean) {
  const date = period.date
  if (period.label) return period.label
  return quarterly ? `${date.slice(0, 4)}Q${Math.round(Number(date.slice(5, 7)) / 3)}` : date.slice(0, 4)
}

// useWidth 用回调 ref 测量宽度：元素晚于组件出现（比如图表先因为没数据返回 null）时也能开始测量。
function useWidth(fallback: number) {
  const [node, setNode] = useState<HTMLDivElement | null>(null)
  const [width, setWidth] = useState(fallback)
  useEffect(() => {
    if (!node) return
    const observer = new ResizeObserver(entries => setWidth(Math.max(280, entries[0].contentRect.width)))
    observer.observe(node)
    return () => observer.disconnect()
  }, [node])
  return [setNode, width, node] as const
}

// 悬停提示：数值在前、名称在后，和图例的层级相反。
interface Tip { x: number; y: number; title: string; rows: { key: string; label: string; value: string; color?: string }[] }
function Tooltip({ tip }: { tip: Tip | null }) {
  if (!tip) return null
  return <div className="chart-tooltip" style={{ left: tip.x, top: tip.y }} role="status">
    <div className="chart-tooltip-title">{tip.title}</div>
    {tip.rows.map(row => <div key={row.key} className="chart-tooltip-row">{row.color && <span className="chart-key-line" style={{ background: row.color }} />}<strong>{row.value}</strong><span>{row.label}</span></div>)}
  </div>
}

function tipAt(event: PointerEvent, box: HTMLElement | null, tip: Omit<Tip, 'x' | 'y'>): Tip {
  const bounds = box?.getBoundingClientRect()
  return { ...tip, x: event.clientX - (bounds?.left ?? 0) + 14, y: event.clientY - (bounds?.top ?? 0) + 14 }
}

function Sankey({ bridge, base, unit }: { bridge: FinancialBridge; base: number; unit: string }) {
  const [box, width, boxNode] = useWidth(900)
  const [tip, setTip] = useState<Tip | null>(null)
  const [active, setActive] = useState<string | null>(null)
  const merged = sankeySteps(bridge)
  const nodeCount = Math.max(...merged.steps.map(step => step.items.length + 2), 4)
  const height = Math.max(260, nodeCount * 30), top = 36
  const layout = buildSankey(merged, height)
  if (typeof layout === 'string') return <div className="empty-state">{layout}</div>
  const W = Math.max(width, 640), nodeW = 12, labelSpace = 150
  const x = (col: number) => col * (W - nodeW - labelSpace) / Math.max(1, layout.columns - 1)
  const share = (value: number) => base > 0 ? ` · 占收入 ${pct(value / base * 100)}` : ''
  return <div className="chart-box" ref={box}>
    <div className="table-scroll"><svg className="sankey" width={W} height={height + top + 8} role="img" aria-label="利润构成桑基图，从收入流向各项支出和利润">
      <g transform={`translate(0 ${top})`}>
      {layout.links.map((link, i) => {
        const x0 = x(link.source.col) + nodeW, x1 = x(link.target.col), mid = (x0 + x1) / 2
        const d = `M${x0},${link.sy} C${mid},${link.sy} ${mid},${link.ty} ${x1},${link.ty} L${x1},${link.ty + link.w} C${mid},${link.ty + link.w} ${mid},${link.sy + link.w} ${x0},${link.sy + link.w} Z`
        const id = `l${i}`
        return <path key={id} d={d} fill={roleColor[link.role === 'main' ? 'main' : link.role]} opacity={active === id ? .55 : .28}
          onPointerMove={event => { setActive(id); setTip(tipAt(event, boxNode, { title: `${link.source.label} → ${link.target.label}`, rows: [{ key: 'v', label: '金额', value: yi(link.value) + share(link.value) }] })) }}
          onPointerLeave={() => { setActive(null); setTip(null) }} />
      })}
      {layout.nodes.map(node => <g key={node.id}
        onPointerMove={event => { setActive(node.id); setTip(tipAt(event, boxNode, { title: node.label, rows: [{ key: 'v', label: '金额', value: yi(node.value) + share(node.value) }] })) }}
        onPointerLeave={() => { setActive(null); setTip(null) }}>
        <rect x={x(node.col)} y={node.y} width={nodeW} height={Math.max(node.h, 1)} rx={2} fill={roleColor[node.role]} />
        <rect x={x(node.col)} y={node.y} width={nodeW + 120} height={Math.max(node.h, 16)} fill="transparent" />
        {/* 主干合计的名称放在节点上方，否则会压到流向下一列的链接和节点。 */}
        {node.role === 'main' && node.y === 0
          ? <text className="sankey-label" x={x(node.col)} y={-20}><tspan>{node.label}</tspan><tspan className="sankey-value" x={x(node.col)} dy={14}>{(node.value / 1e8).toFixed(2)}</tspan></text>
          : <text className="sankey-label" x={x(node.col) + nodeW + 6} y={node.y + Math.max(node.h, 16) / 2 + 4}><tspan>{node.label}</tspan><tspan className="sankey-value" dx={6}>{(node.value / 1e8).toFixed(2)}</tspan></text>}
      </g>)}
      </g>
    </svg></div>
    <div className="chart-legend"><span><i style={{ background: roleColor.main }} />收入与利润</span><span><i style={{ background: roleColor.cost }} />成本、费用与损失</span><span><i style={{ background: roleColor.gain }} />其他收入与收益</span><span>数值单位：亿{unit}</span></div>
    <Tooltip tip={tip} />
  </div>
}

function Waterfall({ bridge, base, label, unit }: { bridge: FinancialBridge; base: number; label: string; unit: string }) {
  const [box, width, boxNode] = useWidth(720)
  const [tip, setTip] = useState<Tip | null>(null)
  const rows = waterfallRows(bridge)
  const rowH = 26, labelW = Math.min(190, width * .36), valueW = 92
  const left = labelW, right = width - valueW
  const values = rows.flatMap(row => [row.from, row.to])
  const low = Math.min(0, ...values), high = Math.max(0, ...values)
  const sx = (value: number) => left + (value - low) / ((high - low) || 1) * (right - left)
  const height = rows.length * rowH + 8
  const color = { total: series(0), up: 'var(--up)', down: 'var(--down)' }
  return <div className="chart-box" ref={box}>
    <svg width={width} height={height} role="img" aria-label={label}>
      <line className="chart-axis" x1={sx(0)} x2={sx(0)} y1={0} y2={height - 8} />
      {rows.map((row, i) => {
        const a = sx(Math.min(row.from, row.to)), b = sx(Math.max(row.from, row.to)), y = i * rowH
        return <g key={i} onPointerMove={event => setTip(tipAt(event, boxNode, { title: row.label, rows: [{ key: 'v', label: row.kind === 'total' ? '合计' : row.kind === 'up' ? '增加' : '减少', value: (row.kind === 'up' ? '+' : '') + yi(row.value) + (base > 0 ? ` · 占收入 ${pct(Math.abs(row.value) / base * 100)}` : '') }] }))} onPointerLeave={() => setTip(null)}>
          <rect x={0} y={y} width={width} height={rowH} fill="transparent" />
          <text className={row.kind === 'total' ? 'waterfall-label is-total' : 'waterfall-label'} x={labelW - 10} y={y + 17} textAnchor="end">{row.label}</text>
          <rect x={a} y={y + 5} width={Math.max(b - a, 1)} height={rowH - 10} rx={3} fill={color[row.kind]} />
          {i < rows.length - 1 && row.kind !== 'total' && <line className="waterfall-connector" x1={sx(row.to)} x2={sx(row.to)} y1={y + rowH - 5} y2={y + rowH + 5} />}
          <text className="waterfall-value" x={Math.min(b + 6, right + 4)} y={y + 17}>{(row.kind === 'up' ? '+' : '') + (row.value / 1e8).toFixed(2)}</text>
        </g>
      })}
    </svg>
    <div className="chart-legend"><span><i style={{ background: color.total }} />合计</span><span><i style={{ background: color.up }} />增加</span><span><i style={{ background: color.down }} />减少</span><span>数值单位：亿{unit}</span></div>
    <Tooltip tip={tip} />
  </div>
}

interface Series { label: string; values: (number | null)[] }

// barPath 画一根柱子：远离基线的一端是 4px 圆角，基线一端是直角。
function barPath(x: number, y0: number, y1: number, w: number) {
  const r = Math.min(4, Math.abs(y1 - y0) / 2, w / 2)
  if (y1 <= y0) return `M${x},${y0} V${y1 + r} Q${x},${y1} ${x + r},${y1} H${x + w - r} Q${x + w},${y1} ${x + w},${y1 + r} V${y0} Z`
  return `M${x},${y0} V${y1 - r} Q${x},${y1} ${x + r},${y1} H${x + w - r} Q${x + w},${y1} ${x + w},${y1 - r} V${y0} Z`
}

function PeriodChart({ title, labels, data, kind, format, tick, note }: { title: string; labels: string[]; data: Series[]; kind: 'columns' | 'lines' | 'stack'; format: (value: number) => string; tick: (value: number) => string; note?: string }) {
  const [box, width] = useWidth(420)
  const [index, setIndex] = useState<number | null>(null)
  const present = data.filter(item => item.values.some(value => value !== null))
  if (!present.length) return null
  const height = 220, left = 56, right = width - 10, top = 10, bottom = height - 26
  const n = labels.length, band = (right - left) / n
  const values = present.flatMap(item => item.values.flatMap(value => value === null ? [] : [value]))
  const ticks = kind === 'stack' ? [0, 25, 50, 75, 100] : niceTicks(Math.min(0, ...values), Math.max(0, ...values))
  const low = ticks[0], high = ticks[ticks.length - 1]
  const sy = (value: number) => bottom - (value - low) / ((high - low) || 1) * (bottom - top)
  const cx = (i: number) => left + band * (i + .5)
  const barW = Math.min(24, band * .72 / (kind === 'columns' ? present.length : 1))
  const pick = (event: PointerEvent<SVGSVGElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    setIndex(Math.max(0, Math.min(n - 1, Math.floor(((event.clientX - bounds.left) * width / bounds.width - left) / band))))
  }
  const key = (event: KeyboardEvent<SVGSVGElement>) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
    event.preventDefault()
    setIndex(i => Math.max(0, Math.min(n - 1, (i ?? n) + (event.key === 'ArrowLeft' ? -1 : 1))))
  }
  const totals = labels.map((_, i) => present.reduce((sum, item) => sum + Math.max(item.values[i] ?? 0, 0), 0))
  const shown = index ?? n - 1
  return <div className="chart-box period-chart" ref={box}>
    <div className="section-caption"><h4>{title}</h4></div>
    {present.length > 1 && <div className="chart-legend">{present.map(item => <span key={item.label}>{kind === 'lines' ? <b style={{ background: series(data.indexOf(item)) }} /> : <i style={{ background: series(data.indexOf(item)) }} />}{item.label}</span>)}</div>}
    <svg width={width} height={height} role="img" tabIndex={0} aria-label={`${title}，左右方向键切换报告期`} onPointerMove={pick} onPointerDown={pick} onPointerLeave={() => setIndex(null)} onKeyDown={key} onBlur={() => setIndex(null)}>
      {ticks.map(value => <g key={value}><line className={value === 0 ? 'chart-axis' : 'chart-grid'} x1={left} x2={right} y1={sy(value)} y2={sy(value)} /><text x={left - 6} y={sy(value) + 4} textAnchor="end">{tick(value)}</text></g>)}
      {index !== null && <rect className="chart-hover-band" x={left + band * index} y={top} width={band} height={bottom - top} />}
      {kind === 'columns' && present.map((item, s) => item.values.map((value, i) => value === null ? null :
        <path key={`${s}-${i}`} d={barPath(cx(i) - present.length * (barW + 2) / 2 + s * (barW + 2) + 1, sy(0), sy(value), barW)} fill={series(data.indexOf(item))} />))}
      {kind === 'stack' && labels.map((_, i) => {
        let acc = 0
        return present.map((item, s) => {
          const value = item.values[i]
          if (value === null || value <= 0 || totals[i] <= 0) return null
          const share = value / totals[i] * 100, y0 = sy(acc), y1 = sy(acc + share)
          acc += share
          return <rect key={`${s}-${i}`} x={cx(i) - barW / 2} y={y1 + 1} width={barW} height={Math.max(y0 - y1 - 2, 0)} fill={series(data.indexOf(item))} />
        })
      })}
      {kind === 'lines' && present.map(item => {
        let open = false
        const d = item.values.map((value, i) => { if (value === null) { open = false; return '' } const c = open ? 'L' : 'M'; open = true; return `${c}${cx(i)},${sy(value)}` }).join(' ')
        return <g key={item.label}><path className="period-line" d={d} stroke={series(data.indexOf(item))} />
          {item.values.map((value, i) => value === null ? null : <circle key={i} className="period-dot" cx={cx(i)} cy={sy(value)} r={i === shown ? 4.5 : 3} fill={series(data.indexOf(item))} />)}</g>
      })}
      {labels.map((label, i) => (band >= 44 || (n - 1 - i) % 2 === 0) && <text key={label} x={cx(i)} y={height - 8} textAnchor="middle">{label}</text>)}
    </svg>
    <div className="period-readout"><strong>{labels[shown]}</strong>{present.map(item => <span key={item.label}>{kind === 'lines' ? <b style={{ background: series(data.indexOf(item)) }} /> : <i style={{ background: series(data.indexOf(item)) }} />}{item.label} {item.values[shown] === null ? '—' : kind === 'stack' && totals[shown] > 0 ? `${format(item.values[shown]!)}（${pct(item.values[shown]! / totals[shown] * 100)}）` : format(item.values[shown]!)}</span>)}</div>
    {note && <p className="data-note">{note}</p>}
  </div>
}

function metricSeries(periods: FinancialPeriod[], pairs: [keyof FinancialMetrics, string][]): Series[] {
  return pairs.map(([key, label]) => ({ label, values: periods.map(period => period.metrics[key] ?? null) }))
}

// 资产科目按固定顺序取颜色，某几年缺少某个科目时，其他科目的颜色也不变。
// 美股（SEC）只拆出现金及等价物、其他流动资产和非流动资产。
const assetOrder = ['货币资金', '应收票据及账款', '存货', '固定资产和在建工程', '发放贷款和垫款', '金融投资', '现金及存放央行', '同业资产', '现金及等价物', '其他流动资产', '非流动资产', '其他资产']

function assetSeries(periods: FinancialPeriod[]): Series[] {
  const present = new Set(periods.flatMap(period => period.assets?.map(item => item.label) ?? []))
  const labels = assetOrder.filter(label => present.has(label))
  return labels.map(label => ({ label, values: periods.map(period => period.assets?.find(item => item.label === label)?.value ?? null) }))
}

function Section({ title, aside, children }: { title: string; aside?: ReactNode; children: ReactNode }) {
  return <section className="financial-chart-section" aria-label={title}><div className="section-caption"><h3>{title}</h3>{aside && <span>{aside}</span>}</div>{children}</section>
}

const typeNotes: Record<ChartData['company_type'], string> = {
  general: '毛利 = 营业总收入 − 营业成本；营业总成本里没有单列的科目（如利息支出）合并为“其他营业成本”。',
  bank: '银行的营业收入由利息净收入、手续费净收入和投资收益等构成，利息支出已在利息净收入中扣除。',
  securities: '证券公司的营业收入由手续费净收入（经纪、投行、资管）、投资收益和利息净收入等构成。',
  insurance: '上游没有拆分保险服务费用等主要营业支出，这部分合并在“其他营业支出”里；部分收入同样合并在“其他收入”里。',
}

// FinancialCharts 是财报的图表分析，A 股和美股（代码以 $ 开头，数据来自 SEC XBRL）共用。
export default function FinancialCharts({ code }: { code: string }) {
  const [freq, setFreq] = useState<'annual' | 'quarter'>('annual')
  const [date, setDate] = useState('')
  const load = useCallback((signal: AbortSignal) => (code.startsWith('$') ? usFinancialCharts : financialCharts)(code, freq, signal), [code, freq])
  const resource = useResource(load)
  // 刷新时保留上一次的图，避免页面跳动；但不能在“单季”按钮下面显示年度数据。
  const last = useRef<ChartData | null>(null)
  if (resource.data) last.current = resource.data
  const data = chartsFor(resource.data, last.current, freq === 'quarter')
  // 频率切换按钮始终显示：切到单季失败时，也要能切回年度。
  const freqToggle = <div className="segmented" role="group" aria-label="报告期类型">{(['annual', 'quarter'] as const).map(item => <button key={item} aria-pressed={freq === item} onClick={() => { setFreq(item); setDate('') }}>{item === 'annual' ? '年度' : '单季'}</button>)}</div>
  if (!data) return <div className="financial-charts"><div className="section-toolbar">{freqToggle}</div><ResourceState {...resource} retry={resource.reload} /></div>
  const periods = data.periods
  const labels = periods.map(item => periodLabel(item, data.quarterly))
  const period = periods.find(item => item.date === date) ?? periods[periods.length - 1]
  const base = period.metrics.revenue ?? 0
  const general = data.company_type === 'general'
  const sec = data.source === 'sec'
  const unit = data.currency_name || '元'
  const name = periodLabel(period, data.quarterly)
  return <div className={resource.loading ? 'financial-charts is-refreshing' : 'financial-charts'}>
    <div className="section-toolbar">
      {freqToggle}
      <label>结构图报告期<select value={period.date} onChange={event => setDate(event.target.value)}>{[...periods].reverse().map(item => <option key={item.date} value={item.date}>{periodLabel(item, data.quarterly)}{data.quarterly ? '' : ' 年报'}</option>)}</select></label>
    </div>
    {resource.error && <ResourceState loading={false} error={resource.error} retry={resource.reload} />}
    <p className="data-note">{data.quarterly ? '单季：利润表和现金流量表是该季度的值，资产负债表是季末值。' : '年度：最近 10 个年报。'}{sec
      ? `数据来自 SEC EDGAR 的 XBRL 财务数据，财年按截止日所在年份标注，金额以${unit}列报；每一步都和报表中的合计科目对齐，没有单列或占收入不到 0.5% 的科目合并为“其他”。SEC 数据只有归母净利润，税后到归母净利润之间的少数股东损益、终止经营等合为一项；期初现金是上期末资产负债表的现金，受限现金、汇率变动等差额合为一项。`
      : `数据来自东方财富 F10 财务报表，每一步都和报表中的合计科目对齐，没有单列或占收入不到 0.5% 的科目合并为“其他”。${typeNotes[data.company_type]}`}</p>
    <Section title="利润构成" aside={`${name} · 收入 ${base ? yi(base) : '—'}`}>
      {period.income ? <Sankey key={`${period.date}-s`} bridge={period.income} base={base} unit={unit} /> : <div className="empty-state">这一期缺少利润表合计科目。</div>}
    </Section>
    <Section title="从收入到归母净利润" aside={name}>
      {period.income ? <Waterfall key={`${period.date}-w`} bridge={period.income} base={base} unit={unit} label="利润瀑布图，从收入逐项扣减到归母净利润" /> : <div className="empty-state">这一期缺少利润表合计科目。</div>}
    </Section>
    <Section title="现金变动" aside={name}>
      {period.cash ? <Waterfall key={`${period.date}-c`} bridge={period.cash} base={0} unit={unit} label="现金瀑布图，从期初现金经三类现金流到期末现金" /> : <div className="empty-state">{sec ? '这一期缺少上期末或本期末的现金，或者缺少经营活动现金流。' : '这一期缺少现金流量表的期初或期末现金。'}</div>}
    </Section>
    <Section title="趋势" aside={`${labels[0]} – ${labels[labels.length - 1]}`}>
      <div className="period-chart-grid">
        <PeriodChart title={`收入与利润（亿${unit}）`} labels={labels} kind="columns" format={yi} tick={value => (value / 1e8).toFixed(0)} data={metricSeries(periods, [['revenue', general && !sec ? '营业总收入' : '营业收入'], ['parent_net_profit', '归母净利润'], ['deduct_net_profit', '扣非归母净利润']])} />
        <PeriodChart title="利润率" labels={labels} kind="lines" format={pct} tick={value => `${value}%`} data={metricSeries(periods, general ? [['gross_margin', '毛利率'], ['net_margin', '净利率']] : [['net_margin', '净利率']])} note={sec ? '毛利率 = 毛利 ÷ 营业收入；净利率 = 归母净利润 ÷ 营业收入。' : general ? '毛利率按营业收入计算；净利率 = 净利润 ÷ 营业总收入。' : '净利率 = 净利润 ÷ 营业收入。'} />
        {sec
          ? <PeriodChart title="费用率" labels={labels} kind="lines" format={pct} tick={value => `${value}%`} data={metricSeries(periods, [['sga_ratio', '销售及管理费用率'], ['research_ratio', '研发费用率']])} note="各项费用 ÷ 营业收入；SEC 报表把销售和管理费用合并列报，分开列报的公司这一项为空。" />
          : general
          ? <PeriodChart title="费用率" labels={labels} kind="lines" format={pct} tick={value => `${value}%`} data={metricSeries(periods, [['sale_ratio', '销售费用率'], ['manage_ratio', '管理费用率'], ['research_ratio', '研发费用率'], ['finance_ratio', '财务费用率']])} note="各项费用 ÷ 营业总收入；财务费用为负表示利息收入大于利息支出。" />
          : <PeriodChart title="成本收入比" labels={labels} kind="lines" format={pct} tick={value => `${value}%`} data={metricSeries(periods, [['cost_income_ratio', '成本收入比']])} note="业务及管理费 ÷ 营业收入。" />}
        <PeriodChart title="资产负债率" labels={labels} kind="lines" format={pct} tick={value => `${value}%`} data={metricSeries(periods, [['debt_ratio', '资产负债率']])} note={sec ? '没有申报负债合计时用总资产减股东权益合计。' : undefined} />
        {(general || data.company_type === 'bank') && <PeriodChart title="资产构成" labels={labels} kind="stack" format={yi} tick={value => `${value}%`} data={assetSeries(periods)} note={sec ? '现金及等价物、其他流动资产和非流动资产占总资产的比例；没有流动资产合计的公司（比如银行）只拆出现金。' : `各项资产占总资产的比例，没有单列的合并为其他资产。${data.company_type === 'bank' ? '2018 年新金融工具准则前，金融投资的科目不同，会计入其他资产。' : ''}`} />}
        <PeriodChart title={`现金流（亿${unit}）`} labels={labels} kind="columns" format={yi} tick={value => (value / 1e8).toFixed(0)} data={metricSeries(periods, [['operating_cash', '经营活动'], ['investing_cash', '投资活动'], ['financing_cash', '筹资活动']])} />
        <PeriodChart title="经营现金流 ÷ 净利润" labels={labels} kind="lines" format={times} tick={value => value.toFixed(1)} data={metricSeries(periods, [['cash_to_profit', '经营现金流 ÷ 净利润']])} note={`大于 1 说明利润有现金支撑；净利润小于等于 0 时不计算。${sec ? '美股用归母净利润。' : ''}`} />
      </div>
      <details className="financial-data-table"><summary>数据表</summary>
        <div className="table-scroll"><table className="data-table"><thead><tr><th>指标</th>{labels.map(label => <th key={label}>{label}</th>)}</tr></thead>
          <tbody>{([['revenue', `收入（亿${unit}）`, yi], ['parent_net_profit', `归母净利润（亿${unit}）`, yi], ['deduct_net_profit', `扣非归母净利润（亿${unit}）`, yi], ['gross_margin', '毛利率', pct], ['net_margin', '净利率', pct], ['sale_ratio', '销售费用率', pct], ['manage_ratio', '管理费用率', pct], ['sga_ratio', '销售及管理费用率', pct], ['research_ratio', '研发费用率', pct], ['finance_ratio', '财务费用率', pct], ['cost_income_ratio', '成本收入比', pct], ['debt_ratio', '资产负债率', pct], ['operating_cash', `经营现金流（亿${unit}）`, yi], ['investing_cash', `投资现金流（亿${unit}）`, yi], ['financing_cash', `筹资现金流（亿${unit}）`, yi], ['cash_to_profit', '经营现金流 ÷ 净利润', times]] as [keyof FinancialMetrics, string, (value: number) => string][])
            .filter(([key]) => periods.some(period => period.metrics[key] != null))
            .map(([key, label, format]) => <tr key={key}><th>{label}</th>{periods.map(period => <td key={period.date}>{period.metrics[key] == null ? '—' : format(period.metrics[key]!)}</td>)}</tr>)}</tbody></table></div>
      </details>
    </Section>
  </div>
}
