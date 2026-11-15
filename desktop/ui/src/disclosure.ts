// openDisclosure 返回用 Desktop 外部窗口打开原文链接的点击处理，打开失败时调用 onError；
// 没有 Desktop 桥（浏览器里开发）时按普通链接打开。申报列表、摘要和风险因素变化共用。
export function openDisclosure(onError: () => void) {
  return (link: string) => (event: { preventDefault: () => void }) => {
    if (!window.secOpenDisclosure) return
    event.preventDefault()
    void window.secOpenDisclosure(link).catch(onError)
  }
}
