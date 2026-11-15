import { useState } from 'react'
import { useActionOp, useActions } from '../api/hooks'
import type { Action } from '../api/schemas'
import { actionStatus } from '../lib/format'
import { useUI } from '../store/ui'
import { Badge } from './ui'

/** Plan → confirm → receipt card. The buddy never executes without this click. */
export function ActionCard({ id, fallback }: { id: string; fallback?: Action }) {
  const q = useActions()
  const a = q.data?.actions.find((x) => x.id === id) ?? fallback
  const op = useActionOp()
  const toast = useUI((s) => s.toast)
  const [edit, setEdit] = useState(false)
  if (!a) return <div className="card card-muted">Действие недоступно или отменено</div>
  const st = actionStatus(a)
  const run = (o: 'confirm' | 'cancel') =>
    op.mutate(
      { id: a.id, op: o },
      {
        onSuccess: (res) => {
          if (res.status === 'failed') toast(res.error ?? 'Не удалось выполнить', 'error')
          else if (o === 'confirm') toast('Готово: ' + res.title, 'success')
        },
        onError: (e) => toast(e.message, 'error'),
      },
    )
  return (
    <div className={`card action-card action-${a.status}`} data-testid="action-card">
      <div className="card-head">
        <span className="card-service">{a.view.service}</span>
        <Badge tone={st.tone}>{st.label}</Badge>
      </div>
      <div className="card-title">{a.title}</div>
      {edit && a.type === 'absence.create' ? (
        <EditAbsence action={a} onDone={() => setEdit(false)} />
      ) : (
        <dl className="fields">
          {a.view.fields.map((f) => (
            <div key={f.label} className="field">
              <dt>{f.label}</dt>
              <dd>{f.value}</dd>
            </div>
          ))}
        </dl>
      )}
      {a.view.warnings.length > 0 && (
        <ul className="warnings" aria-label="Предупреждения">
          {a.view.warnings.map((w) => (
            <li key={w}>⚠️ {w}</li>
          ))}
        </ul>
      )}
      <div className="permission">🔒 {a.view.permission}</div>
      {a.view.note && <div className="muted small">{a.view.note}</div>}
      {a.status === 'failed' && <div className="errorbox">{a.error}</div>}
      {a.external_id && (
        <div className="receipt-line">
          Квитанция: <code>{a.external_id}</code>
        </div>
      )}
      {(a.status === 'planned' || a.status === 'failed') && !edit && (
        <div className="card-actions">
          <button className="btn btn-primary btn-sm" disabled={op.isPending} onClick={() => run('confirm')}>
            {op.isPending ? 'Отправляю…' : a.status === 'failed' ? 'Повторить' : a.view.confirm_label}
          </button>
          {a.type === 'absence.create' && a.status === 'planned' && (
            <button className="btn btn-ghost btn-sm" disabled={op.isPending} onClick={() => setEdit(true)}>
              Изменить
            </button>
          )}
          <button className="btn btn-ghost btn-sm" disabled={op.isPending} onClick={() => run('cancel')}>
            Отменить
          </button>
        </div>
      )}
    </div>
  )
}

function EditAbsence({ action, onDone }: { action: Action; onDone: () => void }) {
  const args = parseDates(action)
  const [v, setV] = useState(args)
  const op = useActionOp()
  const toast = useUI((s) => s.toast)
  return (
    <form
      className="edit-form"
      onSubmit={(e) => {
        e.preventDefault()
        op.mutate({ id: action.id, op: 'edit', ...v }, { onSuccess: onDone, onError: (err) => toast(err.message, 'error') })
      }}
    >
      <label>
        С
        <input type="date" value={v.date_from} onChange={(e) => setV({ ...v, date_from: e.target.value, date_to: v.date_to < e.target.value ? e.target.value : v.date_to })} />
      </label>
      <label>
        По
        <input type="date" value={v.date_to} onChange={(e) => setV({ ...v, date_to: e.target.value })} />
      </label>
      <label className="wide">
        Комментарий
        <input value={v.comment} maxLength={300} onChange={(e) => setV({ ...v, comment: e.target.value })} />
      </label>
      <div className="card-actions">
        <button className="btn btn-primary btn-sm" type="submit" disabled={op.isPending}>
          Сохранить
        </button>
        <button className="btn btn-ghost btn-sm" type="button" onClick={onDone}>
          Назад
        </button>
      </div>
    </form>
  )
}

const months = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря']

/** Extracts ISO dates back from the human "Даты" field of an absence plan. */
export function parseDates(a: Action): { date_from: string; date_to: string; comment: string } {
  const year = new Date(a.created_at).getFullYear()
  const datesField = a.view.fields.find((f) => f.label === 'Даты')?.value ?? ''
  const comment = a.view.fields.find((f) => f.label === 'Комментарий')?.value ?? ''
  const found = [...datesField.matchAll(/(\d{1,2}) ([а-я]+)/g)].map((m) => {
    const mon = months.indexOf(m[2]) + 1
    return `${year}-${String(mon).padStart(2, '0')}-${m[1].padStart(2, '0')}`
  })
  return { date_from: found[0] ?? '', date_to: found[1] ?? found[0] ?? '', comment }
}
