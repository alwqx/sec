import { quoteChange, type QuotesState } from '../hooks/useQuotes'
import type { BasicSecurity } from '../types'

function amount(value: number) {
  if (!value) return '—'
  if (value >= 1e8) return `${(value / 1e8).toFixed(2)} 亿`
  if (value >= 1e4) return `${(value / 1e4).toFixed(2)} 万`
  return value.toFixed(0)
}
export interface CompareState { checked: string[]; only: boolean }

const price = (value: number) => value > 0 ? value.toFixed(2) : '—'

// 自选证券的行情表；勾选后可以只看选中的几只，并排比较。
export default function WatchQuotes({ items, state, selected, onSelect, compare, onCompare }: {
  items: BasicSecurity[]; state: QuotesState; selected: BasicSecurity; onSelect: (security: BasicSecurity) => void
  compare: CompareState; onCompare: (compare: CompareState) => void
}) {
  // 已移出自选的证券不再计入。
  const checked = compare.checked.filter(code => items.some(item => item.ex_code === code))
  const onlyChecked = compare.only && checked.length > 0
  if (!items.length) return <div className="empty-state">暂无自选证券。搜索并选中证券后，点击“加入自选”。</div>
  const toggle = (code: string) => onCompare({ ...compare, checked: checked.includes(code) ? checked.filter(item => item !== code) : [...checked, code] })
  const rows = onlyChecked ? items.filter(item => checked.includes(item.ex_code)) : items
  return <div className="watch-quotes">
    <div className="section-toolbar"><label><input type="checkbox" checked={onlyChecked} disabled={!checked.length} onChange={event => onCompare({ ...compare, only: event.target.checked })} />只看选中（{checked.length}）</label>
      <span className="data-note">{state.updatedAt ? `更新于 ${state.updatedAt.toLocaleTimeString('zh-CN', { hour12: false })}` : '正在读取行情…'}</span></div>
    {state.error && <div className="error" role="alert">行情刷新失败，显示的是上一次结果：{state.error}<button onClick={state.reload}>重试</button></div>}
    <div className="table-scroll"><table className="data-table"><thead><tr><th>对比</th><th>证券</th><th>最新</th><th>涨跌</th><th>涨跌幅</th><th>今开</th><th>最高</th><th>最低</th><th>昨收</th><th>成交额</th><th>行情时间</th></tr></thead>
      <tbody>{rows.map(item => {
        const quote = state.quotes.get(item.ex_code.toUpperCase())
        const change = quote && quoteChange(quote)
        return <tr key={item.ex_code} aria-selected={item.ex_code === selected.ex_code}>
          <td><input type="checkbox" aria-label={`对比 ${item.name}`} checked={checked.includes(item.ex_code)} onChange={() => toggle(item.ex_code)} /></td>
          <td><button className="text-link" onClick={() => onSelect(item)}>{item.name}</button><div className="data-note">{item.ex_code}</div></td>
          {quote && change ? <>
            <td className={change.direction}>{price(quote.current)}</td><td className={change.direction}>{change.changeText}</td><td className={change.direction}>{change.percentText}</td>
            <td>{price(quote.open)}</td><td>{price(quote.high)}</td><td>{price(quote.low)}</td><td>{price(quote.yclose)}</td><td>{amount(quote.volume)}</td><td>{quote.trade_date} {quote.time}</td>
          </> : <td colSpan={9} className="data-note">{state.missing.includes(item.ex_code.toUpperCase()) ? '行情不可用' : '—'}</td>}
        </tr>
      })}</tbody></table></div>
    <p className="data-note">红涨绿跌。行情来自新浪，按设置中的刷新间隔更新；港股、美股的币种和延迟与 A 股不同，不换算。</p>
  </div>
}
