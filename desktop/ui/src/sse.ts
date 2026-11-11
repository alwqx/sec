import type { ExplanationStreamEvent, ValuationExplanation, ValuationExplanationMeta } from './types'

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

// done 是唯一成功终态；HTTP 200 后仍可能出现 error 或异常 EOF。
export async function readExplanationStream(response: Response, emit: (event: ExplanationStreamEvent) => void, signal?: AbortSignal): Promise<ValuationExplanation> {
  if (!response.ok) {
    const body = await response.json().catch(() => null)
    throw new Error(body?.error ?? `请求失败 (${response.status})`)
  }
  if (response.headers.get('Content-Type')?.split(';')[0].trim() !== 'text/event-stream' || !response.body) throw new Error('接口没有返回流式响应，请检查版本或网关')
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let result: ValuationExplanation | null = null
  let size = 0
  let terminal = false
  const parser = new SSEParser((type, data) => {
    if (terminal || !['meta', 'status', 'delta', 'done', 'error'].includes(type)) return
    const value = JSON.parse(data)
    if (type === 'error') { terminal = true; throw new Error(value.message || '流式解读失败') }
    if (type === 'done') {
      if (typeof value?.text !== 'string' || !Array.isArray(value?.check?.unsourced) || !Array.isArray(value?.check?.advice_terms)) throw new Error('流式解读结果格式无效')
      terminal = true; result = value as ValuationExplanation
      emit({ type: 'done', result })
    } else if (type === 'meta') emit({ type, meta: value as ValuationExplanationMeta })
    else if (type === 'status' || type === 'delta') {
      if (typeof value?.text !== 'string') throw new Error('流式文本格式无效')
      emit({ type, text: value.text })
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
    if (!result) throw new Error('流式连接提前结束，解读尚未完成，请重试')
    return result
  } finally {
    signal?.removeEventListener('abort', abort)
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}
