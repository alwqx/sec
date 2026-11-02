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
