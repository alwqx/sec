import { useState } from 'react'
import type { SavedDownload as SavedFile } from '../types'

export default function SavedDownload({ file }: { file: SavedFile }) {
  const [error, setError] = useState('')
  return <div className="saved-download" role="status">
    <strong>已保存：{file.filename}</strong><span>{file.path}</span><span>{(file.size / 1024).toLocaleString('zh-CN', { maximumFractionDigits: 1 })} KB</span>
    {window.secOpenDownloadFolder && <button onClick={() => { void window.secOpenDownloadFolder!().catch(() => setError('无法打开下载目录，请按上述路径查看。')) }}>打开下载目录</button>}
    {error && <span>{error}</span>}
  </div>
}
