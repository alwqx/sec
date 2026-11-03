import { Component, type ErrorInfo, type ReactNode } from 'react'
import ResourceState from './ResourceState'

interface Props { children: ReactNode; message?: string }
interface State { failed: boolean }

// 渲染错误只影响边界内的内容。切换证券、栏目或页面时由外层的 key 重置。
export default class ErrorBoundary extends Component<Props, State> {
  state: State = { failed: false }

  static getDerivedStateFromError(): State {
    return { failed: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('页面渲染失败', error, info.componentStack)
  }

  render() {
    if (!this.state.failed) return this.props.children
    return <ResourceState loading={false} error={this.props.message ?? '这部分内容显示失败，其他功能不受影响。'} retry={() => this.setState({ failed: false })} />
  }
}
