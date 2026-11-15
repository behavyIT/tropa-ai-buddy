import { Fragment, useEffect, useRef, type ReactNode } from 'react'
import { useChat, useFeedback, useSendMessage } from '../api/hooks'
import type { Action, Message } from '../api/schemas'
import { expandSlash, hhmm, slashCommands } from '../lib/format'
import { useUI } from '../store/ui'
import { CardView } from './Cards'
import { ErrorBox, Spinner } from './ui'

const noActions: Action[] = []

const starters = ['Что мне делать дальше?', 'Как оформить отгул?', 'Запроси стандартные доступы', 'Хочу рассказать про Sentry', 'К кому обратиться по доступам?']

export function Chat({ firstName }: { firstName: string }) {
  const chat = useChat()
  const { send, cancel, pending, error, clearError } = useSendMessage()
  const draft = useUI((s) => s.draft)
  const setDraft = useUI((s) => s.setDraft)
  const outbox = useUI((s) => s.outbox)
  const takeOutbox = useUI((s) => s.takeOutbox)
  const endRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const messages = chat.data?.messages ?? []

  useEffect(() => {
    if (outbox !== null && !pending) {
      const t = takeOutbox()
      if (t) void send(t)
    }
  }, [outbox, pending, send, takeOutbox])

  useEffect(() => {
    endRef.current?.scrollIntoView?.({ block: 'end' })
  }, [messages.length, pending])

  const submit = () => {
    const t = expandSlash(draft)
    if (!t) return
    setDraft('')
    void send(t)
    inputRef.current?.focus()
  }

  const showSlash = draft.startsWith('/') && !draft.includes(' ')
  return (
    <section className="chat" aria-label="Чат с Тропой">
      <div className="messages" aria-live="polite">
        {chat.isPending && <Spinner />}
        {chat.isError && <ErrorBox error={chat.error} onRetry={() => chat.refetch()} />}
        {chat.isSuccess && messages.length === 0 && (
          <div className="welcome sticker sticker-sky">
            <div className="welcome-title">Привет, {firstName}! Я Тропа — твой AI-бадди.</div>
            <p>
              Знаю маршрут твоей адаптации, отвечаю по материалам команды (с учётом твоих прав), оформляю заявки в корпоративных
              сервисах — только после твоего подтверждения — и могу передать вопрос наставнику без неловкости.
            </p>
          </div>
        )}
        {messages.map((m, i) => (
          <Bubble key={m.id} m={m} actions={noActions} last={i === messages.length - 1} onSuggest={(s) => void send(s)} />
        ))}
        {pending && (
          <>
            <div className="bubble bubble-user">
              <div className="bubble-text">{pending}</div>
            </div>
            <div className="bubble bubble-buddy typing" role="status">
              <Spinner label="Тропа думает" />
              <span className="muted small">Тропа думает…</span>
              <button className="btn btn-link btn-xs" onClick={cancel}>
                Отменить
              </button>
            </div>
          </>
        )}
        {error && (
          <div className="errorbox" role="alert">
            <span>{error.message}</span>
            <button
              className="btn btn-ghost btn-sm"
              onClick={() => {
                clearError()
                void send(error.text)
              }}
            >
              Повторить
            </button>
          </div>
        )}
        <div ref={endRef} />
      </div>
      {messages.length === 0 && !pending && (
        <div className="chips" aria-label="Быстрые вопросы">
          {starters.map((s) => (
            <button key={s} className="chip" onClick={() => void send(s)}>
              {s}
            </button>
          ))}
        </div>
      )}
      {showSlash && (
        <div className="slash" role="listbox" aria-label="Команды">
          {Object.entries(slashCommands)
            .filter(([k]) => k.startsWith(draft.toLowerCase()))
            .map(([k, v]) => (
              <button key={k} role="option" aria-selected={false} className="slash-item" onClick={() => setDraft(k + ' ')}>
                <code>{k}</code> {v}
              </button>
            ))}
        </div>
      )}
      <form
        className="composer"
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <textarea
          ref={inputRef}
          value={draft}
          rows={1}
          maxLength={2000}
          placeholder="Спроси о команде, инструментах или напиши «/» для команд"
          aria-label="Сообщение"
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              submit()
            }
          }}
        />
        <button className="btn btn-primary" type="submit" disabled={!draft.trim() || !!pending} aria-label="Отправить">
          ➤
        </button>
      </form>
    </section>
  )
}

function Bubble({ m, actions, last, onSuggest }: { m: Message; actions: Action[]; last: boolean; onSuggest: (s: string) => void }) {
  const fb = useFeedback()
  if (m.role === 'user') {
    return (
      <div className="bubble bubble-user">
        <div className="bubble-text">{m.text}</div>
        <div className="bubble-time">{hhmm(m.created_at)}</div>
      </div>
    )
  }
  const p = m.payload
  const isAnswer = (p.intent ?? '').startsWith('question') || p.citations.length > 0
  return (
    <div className="bubble bubble-buddy">
      <div className="bubble-text">{renderText(m.text)}</div>
      {p.cards.map((c, i) => (
        <CardView key={i} card={c} actions={actions} />
      ))}
      {p.citations.length > 0 && (
        <div className="citations" aria-label="Источники">
          {p.citations.map((c) => (
            <details key={c.n} className={`cite cite-${c.kind}`}>
              <summary>
                <span className="cite-n">{c.n}</span> {c.title}
                <span className="cite-kind">{c.kind === 'mentor_answer' ? `Ответ наставника${c.author ? ' · ' + c.author : ''}` : 'Тестовая инструкция'}</span>
              </summary>
              <p>{c.snippet}</p>
            </details>
          ))}
        </div>
      )}
      <div className="bubble-foot">
        <span className="bubble-time">{hhmm(m.created_at)}</span>
        {p.mode && p.mode !== 'offline' && <span className="mode">{p.mode === 'llm' ? 'LLM' : 'LLM недоступна → офлайн'}</span>}
        {isAnswer && (
          <span className="feedback">
            <button aria-label="Полезно" aria-pressed={m.feedback === 1} className={m.feedback === 1 ? 'on' : ''} onClick={() => fb.mutate({ id: m.id, helpful: true })}>
              👍
            </button>
            <button aria-label="Не помогло" aria-pressed={m.feedback === -1} className={m.feedback === -1 ? 'on' : ''} onClick={() => fb.mutate({ id: m.id, helpful: false })}>
              👎
            </button>
          </span>
        )}
      </div>
      {last && p.suggestions.length > 0 && (
        <div className="chips">
          {p.suggestions.map((s) => (
            <button key={s} className="chip" onClick={() => onSuggest(s)}>
              {s}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

/** Paragraphs, bullet lines and [n] citation markers — no HTML injection possible. */
export function renderText(text: string): ReactNode {
  return text.split(/\n{2,}/).map((para, i) => (
    <p key={i}>
      {para.split('\n').map((line, j) => (
        <Fragment key={j}>
          {j > 0 && <br />}
          {line.split(/(\[\d+\])/).map((part, k) =>
            /^\[\d+\]$/.test(part) ? (
              <sup key={k} className="cite-mark">
                {part.slice(1, -1)}
              </sup>
            ) : (
              <Fragment key={k}>{part}</Fragment>
            ),
          )}
        </Fragment>
      ))}
    </p>
  ))
}
