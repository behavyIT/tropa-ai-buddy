import { useState } from 'react'
import { useApprovals, useDecide } from '../api/hooks'
import type { Approval } from '../api/schemas'
import { useUI } from '../store/ui'
import { Badge, Empty, ErrorBox, Spinner } from './ui'

/** Lead's queue: decisions are executed in the corporate service on the lead's behalf. */
export function Approvals() {
  const q = useApprovals()
  if (q.isPending) return <Spinner />
  if (q.isError) return <ErrorBox error={q.error} onRetry={() => q.refetch()} />
  return (
    <section className="panel" aria-label="Согласования">
      <div className="panel-head">
        <h2>Согласования</h2>
        <p className="muted">Заявки, которые стажёры оформили через Тропу. Решение уходит в корпоративный сервис от вашего имени.</p>
      </div>
      {q.data.approvals.length === 0 && <Empty>Нет заявок на согласование</Empty>}
      {q.data.approvals.map((a) => (
        <ApprovalRow key={a.action.id} a={a} />
      ))}
    </section>
  )
}

function ApprovalRow({ a }: { a: Approval }) {
  const [comment, setComment] = useState('')
  const d = useDecide()
  const toast = useUI((s) => s.toast)
  const decide = (approve: boolean) =>
    d.mutate(
      { id: a.action.id, approve, comment },
      { onSuccess: () => toast(approve ? 'Согласовано' : 'Отклонено', 'success'), onError: (e) => toast(e.message, 'error') },
    )
  return (
    <div className="card approval">
      <div className="card-head">
        <span className="card-service">{a.requester}</span>
        <Badge tone="purple">{a.kind}</Badge>
      </div>
      <div className="card-title">{a.details}</div>
      {a.action.view.warnings.length > 0 && <div className="small">⚠️ {a.action.view.warnings.join(' ')}</div>}
      <input className="comment" placeholder="Комментарий (необязательно)" value={comment} onChange={(e) => setComment(e.target.value)} aria-label="Комментарий" />
      <div className="card-actions">
        <button className="btn btn-primary btn-sm" disabled={d.isPending} onClick={() => decide(true)}>
          Согласовать
        </button>
        <button className="btn btn-ghost btn-sm" disabled={d.isPending} onClick={() => decide(false)}>
          Отклонить
        </button>
      </div>
    </div>
  )
}
