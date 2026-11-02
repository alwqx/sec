// 行情日期按北京时间。
export function marketToday() {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date())
  const value = (name: string) => parts.find(part => part.type === name)!.value
  return `${value('year')}-${value('month')}-${value('day')}`
}

// 按自然月回溯，月末日期限制为目标月份的最后一天。
export function monthsAgo(months: number, today = marketToday()) {
  const date = new Date(`${today}T00:00:00Z`)
  const day = date.getUTCDate()
  date.setUTCDate(1)
  date.setUTCMonth(date.getUTCMonth() - months)
  const lastDay = new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth() + 1, 0)).getUTCDate()
  date.setUTCDate(Math.min(day, lastDay))
  return date.toISOString().slice(0, 10)
}

// K 线周期。切换周期时换一个合适的默认区间，快捷区间也随周期变化；单次最长年数和后端一致。
export const frequencies = {
  day: { title: '日线', short: '日', months: 3, ranges: [1, 3, 6, 12, 24, 60], maxYears: 5 },
  week: { title: '周线', short: '周', months: 24, ranges: [12, 24, 60, 120], maxYears: 20 },
  month: { title: '月线', short: '月', months: 60, ranges: [60, 120, 240], maxYears: 40 },
}
export type Frequency = keyof typeof frequencies

export function rangeLabel(months: number) {
  return months < 12 ? `近 ${months} 个月` : `近 ${months / 12} 年`
}
