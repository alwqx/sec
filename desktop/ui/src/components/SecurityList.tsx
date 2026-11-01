import type { BasicSecurity, SecurityQuote } from '../types'
import { quoteChange } from '../hooks/useQuotes'

interface Props {
  items: BasicSecurity[]
  selected: BasicSecurity | null
  onSelect: (security: BasicSecurity) => void
  emptyText?: string
  quotes?: Map<string, SecurityQuote>
}

export default function SecurityList({ items, selected, onSelect, emptyText = '输入关键字搜索证券', quotes }: Props) {
  if (items.length === 0) return <div className="list empty">{emptyText}</div>
  return <ul className="list">{items.map(security => { const quote = quotes?.get(security.ex_code.toUpperCase()); const change = quote && quoteChange(quote); return <li key={security.ex_code}>
    <button className={selected?.ex_code === security.ex_code ? 'security-item selected' : 'security-item'}
      aria-pressed={selected?.ex_code === security.ex_code} onClick={() => onSelect(security)}>
      <span className="security-item-top"><span className="name">{security.name}</span><span className="exchange">{security.exchange}</span></span>
      <span className="code">{security.ex_code}</span>
      {quote && change && <span className={`security-item-quote ${change.direction}`}><span>{quote.current > 0 ? quote.current.toFixed(2) : '—'}</span><span>{change.percentText}</span></span>}
    </button>
  </li> })}</ul>
}
