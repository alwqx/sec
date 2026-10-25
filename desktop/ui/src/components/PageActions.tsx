import { useLayoutEffect, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

// 页面操作显示在统一页头，业务状态仍由所属页面管理。
export default function PageActions({ children }: { children: ReactNode }) {
  const [target, setTarget] = useState<HTMLElement | null>(null)
  useLayoutEffect(() => { setTarget(document.getElementById('page-actions')) }, [])
  return target ? createPortal(children, target) : null
}
