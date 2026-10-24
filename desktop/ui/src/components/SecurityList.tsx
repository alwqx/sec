import type { BasicSecurity } from '../types'

interface Props {
  items: BasicSecurity[]
  selected: BasicSecurity | null
  onSelect: (security: BasicSecurity) => void
  emptyText?: string
}

export default function SecurityList({ items, selected, onSelect, emptyText = '输入关键字搜索证券' }: Props) {
  if (items.length === 0) return <div className="list empty">{emptyText}</div>
  return <ul className="list">{items.map(security => <li key={security.ex_code}>
    <button className={selected?.ex_code === security.ex_code ? 'security-item selected' : 'security-item'}
      aria-pressed={selected?.ex_code === security.ex_code} onClick={() => onSelect(security)}>
      <span className="security-item-top"><span className="name">{security.name}</span><span className="exchange">{security.exchange}</span></span>
      <span className="code">{security.ex_code}</span>
    </button>
  </li>)}</ul>
}
