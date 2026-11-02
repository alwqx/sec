export function money(value: number | null | undefined) {
  if (value === null || value === undefined) return '—'
  const abs = Math.abs(value)
  if (abs >= 1e12) return `${(value / 1e12).toFixed(2)} 万亿元`
  if (abs >= 1e8) return `${(value / 1e8).toFixed(2)} 亿元`
  if (abs >= 1e4) return `${(value / 1e4).toFixed(2)} 万元`
  return `${value.toFixed(2)} 元`
}
