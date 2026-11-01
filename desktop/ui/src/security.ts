import type { BasicSecurity } from './types'

// 由 6 位代码推断交易所：60、68 和 900（B 股）是上交所，4、8、92 是北交所，其余是深交所。
export function securityFromCode(code: string, name: string): BasicSecurity | null {
  if (!/^\d{6}$/.test(code)) return null
  const exchange = /^(6|900)/.test(code) ? 'SH' : /^(4|8|92)/.test(code) ? 'BJ' : 'SZ'
  return { name, code, ex_code: exchange + code, security_type: 'stock', exchange: { SH: 'sse', SZ: 'szse', BJ: 'bse' }[exchange] }
}
