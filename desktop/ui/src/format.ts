// unit 是币种单位，比如美股传'美元'。
export function money(value: number | null | undefined, unit = '元') {
  if (value === null || value === undefined) return '—'
  const abs = Math.abs(value)
  if (abs >= 1e12) return `${(value / 1e12).toFixed(2)} 万亿${unit}`
  if (abs >= 1e8) return `${(value / 1e8).toFixed(2)} 亿${unit}`
  if (abs >= 1e4) return `${(value / 1e4).toFixed(2)} 万${unit}`
  return `${value.toFixed(2)} ${unit}`
}

// yi 把金额固定换算成亿、保留两位小数并加千分位，用于单位写在表头的表格；缺失值显示破折号。
export function yi(value: number | null | undefined) {
  if (value === null || value === undefined) return '—'
  return (value / 1e8).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

// tradeAmount 是行情卡片的成交额；基金净值没有成交时是 null，显示破折号，0 是真实的 0。
export function tradeAmount(value: number | null | undefined) {
  if (value === null || value === undefined) return '—'
  if (value >= 1e8) return `${(value / 1e8).toFixed(2)} 亿`
  if (value >= 1e4) return `${(value / 1e4).toFixed(2)} 万`
  return `${value.toFixed(0)}`
}

// tradeShares 是行情卡片的成交量，规则同 tradeAmount。
export function tradeShares(value: number | null | undefined) {
  if (value === null || value === undefined) return '—'
  if (value >= 1e8) return `${(value / 1e8).toFixed(2)} 亿股`
  if (value >= 1e4) return `${(value / 1e4).toFixed(2)} 万股`
  return `${value} 股`
}
