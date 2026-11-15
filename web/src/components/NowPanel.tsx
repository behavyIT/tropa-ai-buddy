import { useActions, useRoute } from '../api/hooks'
import type { Step } from '../api/schemas'
import { actionStatus, timeAgo } from '../lib/format'
import { useUI } from '../store/ui'
import { Badge, Empty, Spinner } from './ui'

/** Right column: next best step, waiting list and receipts. */
export function NowPanel() {
  const route = useRoute()
  const actions = useActions()
  const ask = useUI((s) => s.ask)
  const next = route.data?.next ?? []
  const receipts = (actions.data?.actions ?? []).filter((a) => a.status !== 'planned' && a.status !== 'cancelled')
  return (
    <aside className="now" aria-label="Сейчас">
      {route.isPending ? (
        <Spinner />
      ) : next.length > 0 ? (
        <div className="sticker sticker-mint next-big">
          <div className="eyebrow">Следующий шаг</div>
          <div className="next-title">{next[0].title}</div>
          <div className="next-why">{next[0].why}</div>
          <button className="btn btn-primary btn-sm" onClick={() => ask(`Помоги с пунктом «${next[0].title}»`)}>
            Помоги с этим
          </button>
        </div>
      ) : (
        <div className="sticker sticker-mint next-big">
          <div className="next-title">Все доступные шаги закрыты 🎉</div>
        </div>
      )}
      <StepList title="Потом" steps={next.slice(1)} />
      <StepList title="Ждём" steps={route.data?.waiting ?? []} tone="lilac" />
      <StepList title="Пока ждёшь — можно сделать" steps={route.data?.while_waiting ?? []} tone="sky" />
      <section className="receipts" aria-label="Мои заявки">
        <h3>Мои заявки и квитанции</h3>
        {receipts.length === 0 && <Empty>Здесь появятся заявки, которые Тропа отправит за тебя</Empty>}
        {receipts.map((a) => {
          const st = actionStatus(a)
          return (
            <div key={a.id} className="receipt">
              <div className="receipt-title">{a.title}</div>
              <div className="receipt-meta">
                <Badge tone={st.tone}>{st.label}</Badge>
                <span className="muted small">{a.view.service}</span>
                {a.external_id && <code className="small">{a.external_id}</code>}
                <span className="muted small">{timeAgo(a.updated_at)}</span>
              </div>
            </div>
          )
        })}
      </section>
    </aside>
  )
}

function StepList({ title, steps, tone }: { title: string; steps: Step[]; tone?: string }) {
  if (steps.length === 0) return null
  return (
    <section className={`steplist ${tone ? `steplist-${tone}` : ''}`}>
      <h3>{title}</h3>
      <ul>
        {steps.map((s) => (
          <li key={s.task_id}>
            <div className="steplist-title">{s.title}</div>
            {s.why && <div className="steplist-why">{s.why}</div>}
          </li>
        ))}
      </ul>
    </section>
  )
}
