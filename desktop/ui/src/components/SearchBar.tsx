import { useState, type FormEvent, type KeyboardEvent } from 'react'

interface Props {
  onSearch: (key: string) => void
  loading: boolean
}

export default function SearchBar({ onSearch, loading }: Props) {
  const [key, setKey] = useState('')

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const k = key.trim()
    if (k) onSearch(k)
  }

  // 桌面 WebView 未注册系统 Edit 菜单，Cmd/Ctrl+A 的默认全选不生效，
  // 这里手动全选输入框内容。
  const selectAll = (e: KeyboardEvent<HTMLInputElement>) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a') {
      e.preventDefault()
      e.currentTarget.select()
    }
  }

  return (
    <form className="search-bar" onSubmit={submit}>
      <input
        value={key}
        onChange={(e) => setKey(e.target.value)}
        onKeyDown={selectAll}
        placeholder="搜索证券：代码 / 名称 / 拼音，如 招商银行、600036、zgpa"
        autoFocus
      />
      <button type="submit" disabled={loading}>
        {loading ? '搜索中…' : '搜索'}
      </button>
    </form>
  )
}
