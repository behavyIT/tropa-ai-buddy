import { useActions } from '../api/hooks'
import type { Action, Card } from '../api/schemas'
import { actionStatus } from '../lib/format'
import { useUI } from '../store/ui'
import { ActionCard } from './ActionCard'
import { Badge } from './ui'

export function CardView({ card, actions }: { card: Card; actions: Action[] }) {
  switch (card.type) {
    case 'action':
      return <ActionCard id={card.action_id ?? ''} fallback={actions.find((a) => a.id === card.action_id)} />
    case 'steps':
      return <StepsCard card={card} />
    case 'person':
      return <PersonCard card={card} />
    case 'teachback':
      return <TeachbackCard card={card} />
    case 'requests':
      return <RequestsCard ids={card.action_ids} />
  }
}

function StepsCard({ card }: { card: Card }) {
  const ask = useUI((s) => s.ask)
  return (
    <div className="card steps-card">
      <div className="card-service">{card.title}</div>
      <ol>
        {card.steps.map((s) => (
          <li key={s.task_id}>
            <button className="linklike" onClick={() => ask(`Помоги с пунктом «${s.title}»`)}>
              {s.title}
            </button>
            {s.why && <div className="muted small">{s.why}</div>}
          </li>
        ))}
      </ol>
    </div>
  )
}

function PersonCard({ card }: { card: Card }) {
  const p = card.person
  if (!p) return null
  return (
    <div className="card person-card">
      <div className="avatar" aria-hidden>
        {p.name
          .split(' ')
          .map((x) => x[0])
          .join('')}
      </div>
      <div>
        <div className="card-title">{p.name}</div>
        <div className="muted small">
          {p.title} · {p.contact}
        </div>
        {p.topic && <Badge tone="blue">{p.topic}</Badge>}
        {p.channel && <div className="small">Канал: {p.channel}</div>}
      </div>
    </div>
  )
}

function TeachbackCard({ card }: { card: Card }) {
  const t = card.teachback
  if (!t) return null
  return (
    <div className={`card teachback ${t.passed ? 'teachback-pass' : ''}`}>
      <div className="card-head">
        <span className="card-service">«Расскажи о {t.title}»</span>
        <Badge tone={t.passed ? 'green' : 'yellow'}>{t.passed ? 'Засчитано' : 'Почти'}</Badge>
      </div>
      <div className="score" aria-label={`Раскрыто ${t.score}%`}>
        <div className="score-fill" style={{ width: `${t.score}%` }} />
        <span>{t.score}%</span>
      </div>
      <ul className="concepts">
        {t.covered.map((c) => (
          <li key={c.id} className="concept-ok">
            ✓ {c.label}
          </li>
        ))}
        {t.missing.map((c) => (
          <li key={c.id} className="concept-miss">
            <span>○ {c.label}</span>
            {c.hint && <span className="hint">{c.hint}</span>}
          </li>
        ))}
      </ul>
    </div>
  )
}

function RequestsCard({ ids }: { ids: string[] }) {
  const q = useActions()
  const list = (q.data?.actions ?? []).filter((a) => ids.includes(a.id))
  return (
    <div className="card">
      <div className="card-service">Мои заявки</div>
      {list.map((a) => (
        <div key={a.id} className="req-row">
          <span>{a.title}</span>
          <Badge tone={actionStatus(a).tone}>{actionStatus(a).label}</Badge>
        </div>
      ))}
    </div>
  )
}
