import { useInsights } from '../api/hooks'
import { ErrorBox, Progress, Spinner } from './ui'

const num = (v: number) => v.toLocaleString('ru-RU', { maximumFractionDigits: 1 })

/** «Тропинки» — desire paths: where interns really get stuck vs. the planned route. */
export function Insights() {
  const q = useInsights()
  if (q.isPending) return <Spinner />
  if (q.isError) return <ErrorBox error={q.error} onRetry={() => q.refetch()} />
  const r = q.data
  const m = r.metrics
  const maxHot = Math.max(1, ...r.hotspots.map((h) => h.questions + 3 * h.waiting + 5 * h.overdue))
  const tiles = [
    { label: 'Ответов без наставника', value: `${m.self_serve_rate}%`, note: `${m.answered} из ${m.answered + m.unanswered}`, tone: 'mint' },
    { label: 'Сэкономлено наставникам', value: `${num(m.mentor_minutes_saved / 60)} ч`, note: `${m.mentor_minutes_saved} мин (оценка)`, tone: 'sky' },
    { label: 'Действий через сервисы', value: String(m.actions_executed), note: m.actions_failed ? `сбоев: ${m.actions_failed}` : 'без сбоев', tone: 'lilac' },
    { label: 'Принято «Расскажи о…»', value: String(m.teachbacks_passed), note: 'наставник не тратил время', tone: 'pink' },
    { label: 'Вопросов наставнику', value: String(m.escalated), note: `объединено ${m.merged}, в базу ${m.published}`, tone: 'mint' },
    { label: 'Ожидание доступа', value: m.avg_access_wait_hours ? `${num(m.avg_access_wait_hours)} ч` : '—', note: 'с учётом прошлой когорты', tone: 'sky' },
  ]
  return (
    <section className="panel insights" aria-label="Тропинки">
      <div className="panel-head">
        <h2>Тропинки · {r.scope}</h2>
        <p className="muted">
          Как в ландшафтном дизайне: плановые дорожки — это чек-лист, а протоптанные тропинки показывают, где стажёры на самом деле
          застревают. По ним видно, что чинить в онбординге.
        </p>
      </div>
      <div className="tiles">
        {tiles.map((t) => (
          <div key={t.label} className={`tile sticker-${t.tone}`}>
            <div className="tile-value">{t.value}</div>
            <div className="tile-label">{t.label}</div>
            <div className="tile-note">{t.note}</div>
          </div>
        ))}
      </div>
      <div className="two-col">
        <div>
          <h3>Где застревают</h3>
          {r.hotspots.map((h) => (
            <div key={h.task_id} className="hot">
              <div className="hot-title">{h.title}</div>
              <Progress value={(100 * (h.questions + 3 * h.waiting + 5 * h.overdue)) / maxHot} label={h.title} />
              <div className="small muted">
                вопросов: {h.questions} · ждут: {h.waiting} · просрочено: {h.overdue}
              </div>
              {h.advice && <div className="small advice">💡 {h.advice}</div>}
            </div>
          ))}
        </div>
        <div>
          <h3>Пробелы базы знаний</h3>
          <table className="gaps">
            <thead>
              <tr>
                <th>Тема</th>
                <th>Раз</th>
                <th>Статус</th>
              </tr>
            </thead>
            <tbody>
              {r.gaps.map((g, i) => (
                <tr key={i}>
                  <td>{g.topic}</td>
                  <td>{g.count}</td>
                  <td>{g.status}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <h3>Стажёры</h3>
          {r.interns.map((i) => (
            <div key={i.id} className="small">
              {i.name}: {i.done}/{i.total} ({i.percent}%){i.overdue ? `, просрочено ${i.overdue}` : ''}
            </div>
          ))}
        </div>
      </div>
      <details className="method">
        <summary>Как проверяем эффект (дизайн эксперимента)</summary>
        <ul>
          <li>
            <b>North Star:</b> время до самостоятельности — дни до первого принятого MR и закрытия этапа «Недели 2–4».
          </li>
          <li>
            <b>Нагрузка наставника:</b> рутинные обращения на стажёра в неделю и часы наставника за программу (база: 40–48 ч за 6 мес.).
          </li>
          <li>
            <b>Качество:</b> доля ответов без эскалации, 👍/👎, повторные вопросы после ответа, 0 утечек по правам.
          </li>
          <li>
            <b>Дизайн:</b> поэтапный запуск по командам через feature flags (stepped-wedge): команды без Тропы — контроль. Малые когорты →
            бутстреп-интервалы и сравнение медиан, плюс опрос стажёров и дневник времени наставника.
          </li>
        </ul>
      </details>
      <ul className="assumptions">
        {r.assumptions.map((a) => (
          <li key={a} className="small muted">
            {a}
          </li>
        ))}
      </ul>
    </section>
  )
}
