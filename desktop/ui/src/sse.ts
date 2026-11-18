import type { AskAnswer, AskStep, AskStreamEvent, ExplanationStreamEvent, ValuationExplanation, ValuationExplanationMeta } from './types'

const maxEvent = 1 << 20
const maxResponse = 16 << 20

// 网络分片与事件无关；保留半行，接受 LF、CRLF、CR 和多行 data，不自动重连。
export class SSEParser {
  private buffer = ''
  private kind = ''
  private data: string[] = []
  private size = 0
  private first = true
  private skipLF = false
  private encoder = new TextEncoder()
  private emit: (type: string, data: string) => void
  constructor(emit: (type: string, data: string) => void) { this.emit = emit }
  push(text: string, final = false) {
    this.buffer += text
    let offset = 0
    for (let i = 0; i < this.buffer.length; i++) {
      const char = this.buffer[i]
      if (this.skipLF) {
        this.skipLF = false
        if (char === '\n') { offset = i + 1; continue }
      }
      if (char !== '\r' && char !== '\n') continue
      this.line(this.buffer.slice(offset, i))
      // CR 已完成这一行；可选的 LF 即使在下一个网络分片也只跳过一次。
      this.skipLF = char === '\r'
      offset = i + 1
    }
    this.buffer = this.buffer.slice(offset)
    if (this.buffer.length > maxEvent) throw new Error('流式事件超过大小限制')
    if (final && (this.buffer || this.data.length)) throw new Error('流式事件不完整')
  }
  private line(raw: string) {
    const line = this.first ? raw.replace(/^\uFEFF/, '') : raw
    this.first = false
    this.size += this.encoder.encode(line).length + 1
    if (this.size > maxEvent) throw new Error('流式事件超过大小限制')
    if (!line) {
      const kind = this.kind || 'message'
      const data = this.data
      this.kind = ''; this.data = []; this.size = 0
      if (data.length) this.emit(kind, data.join('\n'))
      return
    }
    if (line.startsWith(':')) return
    const colon = line.indexOf(':')
    const field = colon < 0 ? line : line.slice(0, colon)
    const value = colon < 0 ? '' : line.slice(colon + 1).replace(/^ /, '')
    if (field === 'event') this.kind = value
    if (field === 'data') this.data.push(value)
  }
}

// readEventStream 读取 SSE 响应，把每个事件交给 handle；handle 返回 true 表示到了成功终态，停止读取，抛出错误表示失败终态。
// HTTP 200 后仍可能出现 error 事件或异常 EOF：没到终态连接就结束时抛出 incomplete。
async function readEventStream(response: Response, handle: (type: string, data: string) => boolean, incomplete: string, signal?: AbortSignal): Promise<void> {
  if (!response.ok) {
    const body = await response.json().catch(() => null)
    throw new Error(body?.error ?? `请求失败 (${response.status})`)
  }
  if (response.headers.get('Content-Type')?.split(';')[0].trim() !== 'text/event-stream' || !response.body) throw new Error('接口没有返回流式响应，请检查版本或网关')
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let size = 0
  let terminal = false
  const parser = new SSEParser((type, data) => {
    if (terminal) return
    try {
      terminal = handle(type, data)
    } catch (e) {
      terminal = true
      throw e
    }
  })
  const abort = () => { void reader.cancel().catch(() => {}) }
  signal?.addEventListener('abort', abort, { once: true })
  try {
    while (!terminal) {
      signal?.throwIfAborted()
      const { done, value } = await reader.read()
      signal?.throwIfAborted()
      if (done) { parser.push(decoder.decode(), true); break }
      size += value.byteLength
      if (size > maxResponse) throw new Error('流式回复超过大小限制')
      parser.push(decoder.decode(value, { stream: true }))
    }
    if (!terminal) throw new Error(incomplete)
  } finally {
    signal?.removeEventListener('abort', abort)
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}

// done 是唯一成功终态；HTTP 200 后仍可能出现 error 或异常 EOF。
export async function readExplanationStream(response: Response, emit: (event: ExplanationStreamEvent) => void, signal?: AbortSignal): Promise<ValuationExplanation> {
  let result: ValuationExplanation | null = null
  await readEventStream(response, (type, data) => {
    if (!['meta', 'status', 'delta', 'done', 'error'].includes(type)) return false
    const value = JSON.parse(data)
    if (type === 'error') throw new Error(value.message || '流式解读失败')
    if (type === 'done') {
      if (typeof value?.text !== 'string' || !Array.isArray(value?.check?.unsourced) || !Array.isArray(value?.check?.advice_terms)) throw new Error('流式解读结果格式无效')
      result = value as ValuationExplanation
      emit({ type: 'done', result })
      return true
    }
    if (type === 'meta') emit({ type, meta: value as ValuationExplanationMeta })
    else {
      if (typeof value?.text !== 'string') throw new Error('流式文本格式无效')
      emit({ type: type as 'status' | 'delta', text: value.text })
    }
    return false
  }, '流式连接提前结束，解读尚未完成，请重试', signal)
  return result!
}

// readAskStream 读取问答的事件：status 是进度，step 是一次工具调用，done 是完整回答（唯一成功终态），error 是失败终态。
export async function readAskStream(response: Response, emit: (event: AskStreamEvent) => void, signal?: AbortSignal): Promise<AskAnswer> {
  let result: AskAnswer | null = null
  await readEventStream(response, (type, data) => {
    if (!['status', 'step', 'done', 'error'].includes(type)) return false
    const value = JSON.parse(data)
    if (type === 'error') throw new Error(value.message || '问答失败')
    if (type === 'done') {
      if (typeof value?.text !== 'string' || !Array.isArray(value?.steps) || !Array.isArray(value?.check?.unsourced) || !Array.isArray(value?.check?.advice_terms)) throw new Error('回答格式无效')
      result = value as AskAnswer
      emit({ type: 'done', result })
      return true
    }
    if (type === 'step') {
      if (typeof value?.tool !== 'string') throw new Error('工具调用格式无效')
      emit({ type, step: value as AskStep })
    } else {
      if (typeof value?.text !== 'string') throw new Error('流式文本格式无效')
      emit({ type: 'status', text: value.text })
    }
    return false
  }, '连接提前结束，回答尚未完成，请重试', signal)
  return result!
}
