interface Props {
  loading: boolean
  error: string
  retry: () => void
}

export default function ResourceState({ loading, error, retry }: Props) {
  if (loading) return <div className="empty-state" role="status"><span className="loading-dot" />正在加载数据…</div>
  if (error) return <div className="empty-state"><p className="error" role="alert">{error}</p><button onClick={retry}>重新加载</button></div>
  return null
}
