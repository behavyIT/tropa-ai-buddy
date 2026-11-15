import { useState } from 'react'
import { useRoute, useTaskAction } from '../api/hooks'
import type { Stage, Task } from '../api/schemas'
import { taskStatus } from '../lib/format'
import { useUI } from '../store/ui'
import { Badge, ErrorBox, Progress, Spinner } from './ui'

/** The personal onboarding route: stages, statuses and actions per task. */
export function RoutePanel({ user, readOnly = false }: { user?: string; readOnly?: boolean }) {
  const q = useRoute(user)
  if (q.isPending) return <Spinner />
  if (q.isError) return <ErrorBox error={q.error} onRetry={() => q.refetch()} />
  const r = q.data
  return (
    <section className="route" aria-label="Маршрут адаптации">
      <div className="route-head">
        <div>
          <div className="eyebrow">{r.route_title}</div>
          <h2>{user ? r.user_name : 'Мой маршрут'}</h2>
        </div>
        <div className="route-stat">
          <strong>{r.percent}%</strong>
          <span>
            {r.done} из {r.total}
          </span>
        </div>
      </div>
      <Progress value={r.percent} label="Прогресс маршрута" />
      {r.overdue > 0 && <div className="overdue-note">Просрочено пунктов: {r.overdue}</div>}
      <div className="stages">
        {r.stages.map((s) => (
          <StageBlock key={s.id} stage={s} user={user} readOnly={readOnly} />
        ))}
      </div>
    </section>
  )
}

function StageBlock({ stage, user, readOnly }: { stage: Stage; user?: string; readOnly: boolean }) {
  const [open, setOpen] = useState(stage.current)
  return (
    <div className={`stage ${stage.current ? 'stage-current' : ''}`}>
      <button className="stage-head" aria-expanded={open} onClick={() => setOpen(!open)}>
        <span className="stage-dot" data-done={stage.done === stage.total} />
        <span className="stage-title">{stage.title}</span>
        <span className="stage-count">
          {stage.done}/{stage.total}
        </span>
      </button>
      {open && (
        <ul className="tasks">
          {stage.tasks.map((t) => (
            <TaskRow key={t.id} task={t} user={user} readOnly={readOnly} />
          ))}
        </ul>
      )}
    </div>
  )
}

function TaskRow({ task, user, readOnly }: { task: Task; user?: string; readOnly: boolean }) {
  const op = useTaskAction()
  const ask = useUI((s) => s.ask)
  const toast = useUI((s) => s.toast)
  const st = taskStatus[task.status]
  const run = (o: 'start' | 'complete' | 'nudge', okText: string) =>
    op.mutate({ task: task.id, op: o, user }, { onSuccess: () => toast(okText, 'success'), onError: (e) => toast(e.message, 'error') })
  return (
    <li className={`task task-${task.status}`}>
      <div className="task-main">
        <span className={`task-check task-check-${task.status}`} aria-hidden />
        <div className="task-body">
          <div className="task-title">{task.title}</div>
          <div className="task-meta">
            <Badge tone={st.tone}>{st.label}</Badge>
            <span className={task.overdue ? 'due due-overdue' : 'due'}>до {task.due_date}</span>
            {task.owner !== 'intern' && <span className="owner">отвечает: {task.owner_name}</span>}
          </div>
          {task.waiting_for && <div className="task-wait">{task.waiting_for}</div>}
          {!readOnly && (
            <div className="task-actions">
              {task.can_start && (
                <button className="btn btn-ghost btn-xs" disabled={op.isPending} onClick={() => run('start', 'Отметили, что ты начал')}>
                  Начать
                </button>
              )}
              {task.can_complete && (
                <button className="btn btn-ghost btn-xs" disabled={op.isPending} onClick={() => run('complete', 'Пункт отмечен в чек-листе')}>
                  Готово
                </button>
              )}
              {task.kind === 'teachback' && task.status !== 'done' && task.status !== 'blocked' && !user && (
                <button className="btn btn-sticker btn-xs" onClick={() => ask(`Хочу рассказать про ${task.title.replace('Расскажи о ', '')}`)}>
                  Сдать в чате
                </button>
              )}
              {task.kind === 'access' && task.status === 'todo' && !user && (
                <button className="btn btn-sticker btn-xs" onClick={() => ask(`Запроси доступ: ${task.systems.join(', ')}`)}>
                  Запросить
                </button>
              )}
              {task.can_nudge && task.status !== 'done' && (
                <button className="btn btn-ghost btn-xs" disabled={op.isPending} onClick={() => run('nudge', 'Напоминание отправлено — без неловкости')}>
                  Напомнить
                </button>
              )}
              {task.doc_title && !user && (
                <button className="btn btn-link btn-xs" onClick={() => ask(`Расскажи подробнее: ${task.doc_title}`)}>
                  Материал
                </button>
              )}
            </div>
          )}
        </div>
      </div>
    </li>
  )
}
