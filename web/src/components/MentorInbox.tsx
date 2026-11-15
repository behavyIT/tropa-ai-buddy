import { useState } from 'react'
import { useAnswer, useQuestions } from '../api/hooks'
import type { Question } from '../api/schemas'
import { timeAgo } from '../lib/format'
import { useUI } from '../store/ui'
import { Badge, Empty, ErrorBox, Spinner } from './ui'

/** Mentor digest: batched, de-duplicated questions; answers can become team knowledge. */
export function MentorInbox() {
  const q = useQuestions()
  if (q.isPending) return <Spinner />
  if (q.isError) return <ErrorBox error={q.error} onRetry={() => q.refetch()} />
  const open = q.data.questions.filter((x) => x.status === 'open')
  const done = q.data.questions.filter((x) => x.status !== 'open')
  return (
    <section className="panel" aria-label="Вопросы стажёров">
      <div className="panel-head">
        <h2>Сводка вопросов</h2>
        <p className="muted">
          Тропа собирает несрочные вопросы стажёров в одну сводку и объединяет похожие. Ответ можно сохранить в базу знаний — следующие
          стажёры получат его без вас.
        </p>
      </div>
      {open.length === 0 && <Empty>Новых вопросов нет — стажёры справляются сами 🙌</Empty>}
      {open.map((x) => (
        <QuestionCard key={x.id} q={x} />
      ))}
      {done.length > 0 && (
        <>
          <h3>Отвечено</h3>
          {done.map((x) => (
            <div key={x.id} className="card card-muted">
              <div className="card-title">{x.text}</div>
              <div className="small">{x.answer}</div>
              {x.card_id && <Badge tone="green">в базе знаний</Badge>}
            </div>
          ))}
        </>
      )}
    </section>
  )
}

function QuestionCard({ q }: { q: Question }) {
  const [text, setText] = useState('')
  const [publish, setPublish] = useState(true)
  const answer = useAnswer()
  const toast = useUI((s) => s.toast)
  return (
    <div className={`card question ${q.urgent ? 'question-urgent' : ''}`}>
      <div className="card-head">
        <span className="card-service">
          {q.asker_names.join(', ')} · {timeAgo(q.created_at)}
        </span>
        <span>
          {q.urgent && <Badge tone="red">срочно</Badge>} {q.ask_count > 1 && <Badge tone="purple">спросили {q.ask_count}</Badge>}
        </span>
      </div>
      <div className="card-title">{q.text}</div>
      {q.context && <div className="muted small">{q.context}</div>}
      <form
        className="answer-form"
        onSubmit={(e) => {
          e.preventDefault()
          answer.mutate({ id: q.id, answer: text, publish }, { onSuccess: () => toast('Ответ отправлен стажёру', 'success'), onError: (err) => toast(err.message, 'error') })
        }}
      >
        <textarea value={text} onChange={(e) => setText(e.target.value)} placeholder="Ваш ответ" aria-label="Ответ" rows={3} maxLength={4000} />
        <label className="check">
          <input type="checkbox" checked={publish} onChange={(e) => setPublish(e.target.checked)} />
          Сохранить ответ в базу знаний команды
        </label>
        <button className="btn btn-primary btn-sm" type="submit" disabled={!text.trim() || answer.isPending}>
          Ответить
        </button>
      </form>
    </div>
  )
}
