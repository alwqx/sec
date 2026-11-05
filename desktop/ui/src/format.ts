// unit 是币种单位，比如美股传'美元'。
export function money(value: number | null | undefined, unit = '元') {
  if (value === null || value === undefined) return '—'
  const abs = Math.abs(value)
  if (abs >= 1e12) return `${(value / 1e12).toFixed(2)} 万亿${unit}`
  if (abs >= 1e8) return `${(value / 1e8).toFixed(2)} 亿${unit}`
  if (abs >= 1e4) return `${(value / 1e4).toFixed(2)} 万${unit}`
  return `${value.toFixed(2)} ${unit}`
}
