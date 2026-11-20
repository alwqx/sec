import { useEffect, useRef, useState, type FormEvent } from 'react'
import { streamAsk } from '../api'
import { answeredSteps, askCode, askMeta, maxAskHistory, maxAskQuestion, newConversationID, stepLine } from '../aiAsk'
import { useAIModels } from '../hooks/useAIModels'
import type { AskAnswer, AskStep, BasicSecurity } from '../types'
import SummaryModelPicker from './SummaryModelPicker'
import { SummaryResultView } from './SummaryPanel'

// Turn 是对话里的一个问题和它的回答，只保存在内存里。
interface Turn {
  id: number
  question: string
  code: string
  status: string
  steps: AskStep[]
  answer?: AskAnswer
  error?: string
}

// AIChat 是对话面板：模型调用 sec 的只读工具回答问题，和 sec ask 共用后端。追问时后端带上同一对话最近几轮的问答
// （按对话 id 保存在 Desktop 服务端，页面只提交 id），「新对话」换一个 id 重新开始；对话只保存在内存，关闭窗口即清空。
// 研究页选中的证券默认作为上下文带入，可以去掉。一次只回答一个问题，回答时逐条显示调用的工具、参数和截止日期，回答下方是数字核对结果、说明和免责声明。active 是面板当前是否显示：
// 面板切走后只是隐藏，再次显示时重新读取模型配置（设置页可能改过），并回到最新的问答。
export default function AIChat({ security, active }: { security: BasicSecurity | null; active: boolean }) {
  const models = useAIModels()
  const { profile, unavailable, reload } = models
  const [question, setQuestion] = useState('')
  const [includeSecurity, setIncludeSecurity] = useState(true)
  const [turns, setTurns] = useState<Turn[]>([])
  const [conversation, setConversation] = useState(newConversationID)
  const [busy, setBusy] = useState(false)
  const controller = useRef<AbortController | null>(null)
  const nextID = useRef(1)
  const lastTurn = useRef<HTMLElement | null>(null)
  const shown = useRef(false)
  // 换了证券时重新默认带入。
  useEffect(() => setIncludeSecurity(true), [security?.ex_code])
  useEffect(() => () => controller.current?.abort(), [])
  useEffect(() => {
    if (!active) return
    if (shown.current) reload()
    shown.current = true
    lastTurn.current?.scrollIntoView({ block: 'start' })
  }, [active, reload])
  // 新的问题滚动到可见位置，回答在它下面展开。
  const lastID = turns.length ? turns[turns.length - 1].id : 0
  useEffect(() => { lastTurn.current?.scrollIntoView({ block: 'start' }) }, [lastID])

  const update = (id: number, change: (turn: Turn) => Turn) => setTurns(all => all.map(turn => turn.id === id ? change(turn) : turn))
  const blocked = !profile || !!unavailable
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    const text = question.trim()
    if (blocked || busy || !text) return
    const id = nextID.current++
    const code = askCode(security, includeSecurity)
    const request = new AbortController()
    controller.current = request
    setBusy(true)
    setQuestion('')
    setTurns(all => [...all, { id, question: text, code, status: '正在请求模型…', steps: [] }])
    try {
      const answer = await streamAsk({ question: text, code, profile: profile!.name, conversation }, event => {
        if (event.type === 'status') update(id, turn => ({ ...turn, status: event.text }))
        else if (event.type === 'step') update(id, turn => ({ ...turn, steps: [...turn.steps, event.step], status: `已完成 ${turn.steps.length + 1} 次工具调用，等待其余工具或模型…` }))
      }, request.signal)
      update(id, turn => ({ ...turn, answer, steps: answeredSteps(turn.steps, answer) }))
    } catch (e) {
      const message = request.signal.aborted ? '已取消' : e instanceof Error ? e.message : String(e)
      update(id, turn => ({ ...turn, error: message }))
    } finally {
      if (controller.current === request) controller.current = null
      setBusy(false)
    }
  }
  const cancel = () => controller.current?.abort()

  return <div className="page-panel ai-chat">
    <SummaryModelPicker models={models} action="提问" sends="问题、带入的证券代码和工具查到的行情、财务等数据"
      extra={`追问时带上本对话最近 ${maxAskHistory} 轮的问答和调用过的工具（不发送工具结果，需要时重新查询）；点「新对话」重新开始。对话只保存在本窗口，关闭后清空。`} />
    <div className="chat-log" aria-label="对话记录">
      {!turns.length && <p className="data-note">可以问行情、K 线、财务、估值、公告和宏观数据，比如「现在的估值处于历史什么位置？」「最近 5 年的营业收入和净利润？」「美国最新的 CPI 同比是多少？」。模型只用 sec 查到的数据回答，不给投资建议。</p>}
      {turns.map(turn => <article key={turn.id} ref={turn.id === lastID ? lastTurn : undefined} className="chat-turn" aria-label={`问题：${turn.question}`}>
        <p className="chat-question">{turn.code && <span className="chat-chip">{turn.code}</span>}{turn.question}</p>
        {turn.steps.length > 0 && <ol className="chat-steps" aria-label="工具调用">{turn.steps.map((step, index) =>
          <li key={index} className={step.error ? 'is-failed' : ''}>{stepLine(step)}</li>)}</ol>}
        {!turn.answer && !turn.error && <p className="data-note" role="status">{turn.status}</p>}
        {turn.error && <p className="error" role="alert">{turn.error}</p>}
        {turn.answer && <SummaryResultView result={turn.answer} meta={askMeta(turn.answer)}>
          <p className="explanation-text">{turn.answer.text}</p>
          {turn.answer.truncated && <p className="error">输出达到上限，回答不完整{turn.answer.truncation_hint || '。'}</p>}
          {turn.answer.data_dates.length > 0 && <p className="data-note">数据截止：{turn.answer.data_dates.join('；')}</p>}
        </SummaryResultView>}
      </article>)}
    </div>
    <form className="chat-form" onSubmit={event => void submit(event)}>
      {security && <label className="chat-context"><input type="checkbox" checked={includeSecurity} disabled={busy} onChange={event => setIncludeSecurity(event.target.checked)} />带入当前证券 {security.name}（{security.ex_code}）</label>}
      <div className="chat-input">
        <textarea aria-label="问题" rows={2} value={question} maxLength={maxAskQuestion} disabled={blocked} placeholder={unavailable || '输入问题，Enter 发送，Shift+Enter 换行'}
          onChange={event => setQuestion(event.target.value)}
          onKeyDown={event => {
            if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit() }
          }} />
        <button type="submit" disabled={blocked || busy || !question.trim()}>{busy ? '回答中…' : '提问'}</button>
        {busy && <button type="button" onClick={cancel}>取消</button>}
        {!busy && turns.length > 0 && <button type="button" onClick={() => { setTurns([]); setConversation(newConversationID()) }}>新对话</button>}
      </div>
    </form>
  </div>
}
