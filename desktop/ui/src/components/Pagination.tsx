export default function Pagination({ page, total, hasMore, loading, onPage }: {
  page: number; total: number; hasMore: boolean; loading: boolean; onPage: (page: number) => void
}) {
  return <div className="pagination"><span>第 {page} 页 · 共 {total} 条</span><div>
    <button disabled={loading || page === 1} onClick={() => onPage(page - 1)}>上一页</button>
    <button disabled={loading || !hasMore} onClick={() => onPage(page + 1)}>下一页</button>
  </div></div>
}
