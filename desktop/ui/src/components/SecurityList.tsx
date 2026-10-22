import type { BasicSecurity } from '../types'

interface Props {
  items: BasicSecurity[]
  selected: BasicSecurity | null
  onSelect: (s: BasicSecurity) => void
}

export default function SecurityList({ items, selected, onSelect }: Props) {
  if (items.length === 0) {
    return <div className="list empty">输入关键字搜索证券</div>
  }

  return (
    <ul className="list">
      {items.map((s) => (
        <li
          key={s.ex_code}
          className={selected?.ex_code === s.ex_code ? 'item selected' : 'item'}
          onClick={() => onSelect(s)}
        >
          <span className="name">{s.name}</span>
          <span className="code">{s.code}</span>
          <span className="exchange">{s.exchange}</span>
        </li>
      ))}
    </ul>
  )
}
