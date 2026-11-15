import { useTeam } from '../api/hooks'
import { useUI } from '../store/ui'
import { RoutePanel } from './RoutePanel'
import { Empty, ErrorBox, Progress, Spinner } from './ui'

/** Interns visible to a mentor / lead / HR BP with their progress; click to open a route. */
export function TeamPanel() {
  const q = useTeam()
  const view = useUI((s) => s.viewIntern)
  const setView = useUI((s) => s.setViewIntern)
  if (q.isPending) return <Spinner />
  if (q.isError) return <ErrorBox error={q.error} onRetry={() => q.refetch()} />
  return (
    <section className="panel team" aria-label="Стажёры">
      <div className="panel-head">
        <h2>Стажёры</h2>
        <p className="muted">Видно только тех, кого вам позволяет видеть ролевая модель. Переписка стажёров с Тропой приватна.</p>
      </div>
      {q.data.interns.length === 0 && <Empty>Нет стажёров</Empty>}
      <div className="team-grid">
        <div className="team-list">
          {q.data.interns.map((i) => (
            <button key={i.id} className={`intern ${view === i.id ? 'intern-active' : ''}`} onClick={() => setView(i.id)}>
              <div className="intern-head">
                <strong>{i.name}</strong>
                <span className="muted small">
                  {i.team} · день {i.day}
                </span>
              </div>
              <Progress value={i.percent} label={`Прогресс ${i.name}`} />
              <div className="small">
                {i.done} из {i.total}
                {i.overdue > 0 && <span className="due-overdue"> · просрочено {i.overdue}</span>}
              </div>
              {i.next && <div className="small muted">Сейчас: {i.next}</div>}
              {i.waiting.length > 0 && <div className="small">Ждёт: {i.waiting.join('; ')}</div>}
            </button>
          ))}
        </div>
        <div className="team-route">{view ? <RoutePanel user={view} /> : <Empty>Выберите стажёра, чтобы открыть его маршрут</Empty>}</div>
      </div>
    </section>
  )
}
