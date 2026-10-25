import { useState, type FormEvent, type KeyboardEvent } from 'react'

interface Props {
  onSearch: (key: string) => void
  loading: boolean
  initialKey?: string
}

export default function SearchBar({ onSearch, loading, initialKey = '' }: Props) {
  const [key, setKey] = useState(initialKey)

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
        aria-label="搜索证券"
        value={key}
        onChange={(e) => setKey(e.target.value)}
        onKeyDown={selectAll}
        placeholder="名称 / 代码 / 拼音"
        autoFocus
      />
      <button type="submit" disabled={loading}>
        {loading ? '搜索中…' : '搜索'}
      </button>
    </form>
  )
}
