import { useFlags, useSetFlag } from '../api/hooks'
import type { Me } from '../api/schemas'
import { useUI } from '../store/ui'
import { ErrorBox, Spinner } from './ui'

/** Runtime feature flags: a lead switches scenarios for the team, HR BP — globally. */
export function FlagsPanel({ me }: { me: Me }) {
  const q = useFlags()
  const set = useSetFlag()
  const toast = useUI((s) => s.toast)
  const scope = me.user.role === 'hrbp' ? 'global' : `team:${me.user.team_id}`
  if (q.isPending) return <Spinner />
  if (q.isError) return <ErrorBox error={q.error} onRetry={() => q.refetch()} />
  return (
    <section className="panel" aria-label="Фича-флаги">
      <div className="panel-head">
        <h2>Сценарии и фича-флаги</h2>
        <p className="muted">
          {scope === 'global' ? 'Глобальные значения для всех команд.' : `Переопределения для команды «${me.user.team_name}».`} Так сценарии
          раскатываются по командам и проверяются A/B-способом.
        </p>
      </div>
      <ul className="flags">
        {q.data.flags.map((f) => (
          <li key={f.key} className="flag">
            <label className="switch">
              <input
                type="checkbox"
                checked={f.enabled}
                disabled={set.isPending}
                onChange={(e) =>
                  set.mutate({ key: f.key, enabled: e.target.checked, scope }, { onError: (err) => toast(err.message, 'error') })
                }
              />
              <span>{f.description}</span>
            </label>
            <code className="small">{f.key}</code>
            <span className="muted small">источник: {f.source}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}
