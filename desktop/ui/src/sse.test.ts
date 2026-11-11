import assert from 'node:assert/strict'
import test from 'node:test'
import { SSEParser, readExplanationStream } from './sse.ts'
import type { ExplanationStreamEvent, ValuationExplanation } from './types'

const encoder = new TextEncoder()
const result: ValuationExplanation = { ex_code: 'SH600036', name: '招商银行', as_of: '2026-10-30', text: '招商银行解读', disclaimer: '不构成投资建议', model: 'm', profile: 'p', prompt_version: 'v1', created_at: '2026-11-10T00:00:00Z', cached: false, truncated: false, check: { numbers: 0, unsourced: [], advice_terms: [] } }
const frame = (type: string, value: unknown) => `event: ${type}\r\ndata: ${JSON.stringify(value)}\r\n\r\n`
const response = (stream: ReadableStream<Uint8Array>) => new Response(stream, { headers: { 'Content-Type': 'text/event-stream; charset=utf-8' } })

test('SSE 逐字符分片支持 BOM、CRLF、CR、多行 data 和注释', () => {
  for (const newline of ['\n', '\r\n', '\r']) {
    const events: string[][] = []
    const parser = new SSEParser((type, data) => events.push([type, data]))
    const wire = '\uFEFF' + [': ping', 'event: delta', 'data: 招商', 'data: 银行', '', 'data:', '', ''].join(newline)
    for (const char of wire) parser.push(char)
    parser.push('', true)
    assert.deepEqual(events, [['delta', '招商\n银行'], ['message', '']])
  }
})

test('收到首段正文才释放后续响应，中文 UTF-8 按字节分片仍正确', async () => {
  let release!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  const events: ExplanationStreamEvent[] = []
  const body = new ReadableStream<Uint8Array>({
    async start(controller) {
      for (const byte of encoder.encode(frame('delta', { text: '招商银行' }))) controller.enqueue(Uint8Array.of(byte))
      await gate
      controller.enqueue(encoder.encode(frame('done', result)))
      controller.close()
    },
  })
  let timedOut = false
  const timeout = setTimeout(() => { timedOut = true; release() }, 1000)
  try {
    const got = await readExplanationStream(response(body), event => {
      events.push(event)
      if (event.type === 'delta') { assert.equal(event.text, '招商银行'); release() }
    })
    assert.deepEqual(got, result)
    assert.equal(timedOut, false, '首段回调应发生在后续响应释放之前')
    assert.deepEqual(events.map(event => event.type), ['delta', 'done'])
  } finally { clearTimeout(timeout) }
})

test('缓存只返回 done，心跳和未知事件不影响结果', async () => {
  const wire = ': ping\n\n' + frame('future', {}) + frame('done', { ...result, cached: true })
  const got = await readExplanationStream(new Response(wire, { headers: { 'Content-Type': 'text/event-stream' } }), () => {})
  assert.equal(got.cached, true)
})

test('CR 事件立即分发，跨分片的可选 LF 不产生额外空行', () => {
  const events: string[][] = []
  const parser = new SSEParser((type, data) => events.push([type, data]))
  parser.push('event: delta\rdata: 首段\r\r')
  assert.deepEqual(events, [['delta', '首段']], '不等待下一字节或 EOF')
  for (const char of '\nevent: done\r\ndata: 完整\r\ndata: 回复\r\n\r') parser.push(char)
  assert.deepEqual(events, [['delta', '首段'], ['done', '完整\n回复']])
  parser.push('\n')
  parser.push('', true)
  assert.equal(events.length, 2)
})

test('CR 正文和 done 在连接保持打开时到达并完成解读', async () => {
  let controller!: ReadableStreamDefaultController<Uint8Array>
  let canceled = false
  const body = new ReadableStream<Uint8Array>({
    start(value) {
      controller = value
      controller.enqueue(encoder.encode('event: delta\rdata: {"text":"首段"}\r\r'))
    },
    cancel() { canceled = true },
  })
  const abort = new AbortController()
  const timeout = setTimeout(() => abort.abort(), 1000)
  const events: ExplanationStreamEvent[] = []
  try {
    const got = await readExplanationStream(response(body), event => {
      events.push(event)
      // 只在首段被消费后发送结束事件，全程不关闭响应。
      if (event.type === 'delta') controller.enqueue(encoder.encode(`event: done\rdata: ${JSON.stringify(result)}\r\r`))
    }, abort.signal)
    assert.deepEqual(got, result)
    assert.deepEqual(events.map(event => event.type), ['delta', 'done'])
    assert.equal(canceled, true)
  } finally { clearTimeout(timeout) }
})

test('断流、错误事件、无效 JSON、错误格式和非 SSE 均失败', async () => {
  for (const [wire, expected] of [
    [frame('delta', { text: '草稿' }), /提前结束/],
    ['event: delta\ndata: {"text":"草稿"}', /不完整/],
    [frame('error', { message: '模型不可用' }), /模型不可用/],
    ['event: delta\ndata: invalid\n\n', /JSON/],
    [frame('done', { text: '未核对' }), /格式无效/],
  ] as const) {
    await assert.rejects(readExplanationStream(new Response(wire, { headers: { 'Content-Type': 'text/event-stream' } }), () => {}), expected)
  }
  await assert.rejects(readExplanationStream(new Response('{}'), () => {}), /没有返回流式/)
  await assert.rejects(readExplanationStream(new Response('{"error":"请先启用 AI"}', { status: 409 }), () => {}), /请先启用 AI/)
})

test('取消正在等待的响应会关闭 reader，不等待模型结束', async () => {
  let canceled = false
  const abort = new AbortController()
  const body = new ReadableStream<Uint8Array>({ cancel() { canceled = true } })
  const pending = readExplanationStream(response(body), () => {}, abort.signal)
  abort.abort()
  await assert.rejects(pending, { name: 'AbortError' })
  assert.equal(canceled, true)
})

test('回调失败关闭连接，大小限制阻止无界缓冲', async () => {
  let canceled = false
  const body = new ReadableStream<Uint8Array>({
    start(controller) { controller.enqueue(encoder.encode(frame('delta', { text: '草稿' }))) },
    cancel() { canceled = true },
  })
  await assert.rejects(readExplanationStream(response(body), () => { throw new Error('页面已离开') }), /页面已离开/)
  assert.equal(canceled, true)
  const parser = new SSEParser(() => {})
  assert.throws(() => parser.push('data: ' + 'x'.repeat(1 << 20)), /大小限制/)
})
