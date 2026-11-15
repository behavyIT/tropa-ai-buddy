import { useEffect } from 'react'
import { ApiError } from './api/client'
import { useMe } from './api/hooks'
import type { Me } from './api/schemas'
import { Approvals } from './components/Approvals'
import { Chat } from './components/Chat'
import { FlagsPanel } from './components/FlagsPanel'
import { Insights } from './components/Insights'
import { Login } from './components/Login'
import { MentorInbox } from './components/MentorInbox'
import { NowPanel } from './components/NowPanel'
import { RoutePanel } from './components/RoutePanel'
import { TeamPanel } from './components/TeamPanel'
import { TopBar } from './components/TopBar'
import { ErrorBox, Spinner } from './components/ui'
import { setServerNow } from './lib/format'
import { useUI, type Pane } from './store/ui'

export function App() {
  const me = useMe()
  if (me.data) setServerNow(me.data.demo.now)
  if (me.isPending) return <Spinner />
  if (me.isError) {
    if (me.error instanceof ApiError && me.error.status === 401) return <Login />
    return <ErrorBox error={me.error} onRetry={() => me.refetch()} />
  }
  return (
    <div className="app">
      <TopBar me={me.data} />
      {me.data.user.role === 'intern' ? <InternWorkspace me={me.data} /> : <StaffWorkspace me={me.data} />}
      <Toasts />
    </div>
  )
}

const panes: { id: Pane; label: string }[] = [
  { id: 'route', label: 'Маршрут' },
  { id: 'chat', label: 'Чат' },
  { id: 'now', label: 'Сейчас' },
]

function InternWorkspace({ me }: { me: Me }) {
  const pane = useUI((s) => s.pane)
  const setPane = useUI((s) => s.setPane)
  return (
    <>
      <main className={`workspace pane-${pane}`}>
        <div className="col col-route">
          <RoutePanel />
        </div>
        <div className="col col-chat">
          <Chat firstName={me.user.name.split(' ')[0]} />
        </div>
        <div className="col col-now">
          <NowPanel />
        </div>
      </main>
      <nav className="mobile-tabs" aria-label="Разделы">
        {panes.map((p) => (
          <button key={p.id} aria-current={pane === p.id} onClick={() => setPane(p.id)}>
            {p.label}
          </button>
        ))}
      </nav>
    </>
  )
}

const staffTabs: Record<string, { id: string; label: string }[]> = {
  mentor: [
    { id: 'questions', label: 'Вопросы стажёров' },
    { id: 'team', label: 'Подопечные' },
    { id: 'chat', label: 'Чат с Тропой' },
  ],
  lead: [
    { id: 'approvals', label: 'Согласования' },
    { id: 'team', label: 'Команда' },
    { id: 'questions', label: 'Вопросы' },
    { id: 'insights', label: 'Тропинки' },
    { id: 'flags', label: 'Флаги' },
    { id: 'chat', label: 'Чат' },
  ],
  hrbp: [
    { id: 'insights', label: 'Тропинки' },
    { id: 'team', label: 'Стажёры' },
    { id: 'flags', label: 'Флаги' },
    { id: 'chat', label: 'Чат' },
  ],
}

function StaffWorkspace({ me }: { me: Me }) {
  const tabs = staffTabs[me.user.role] ?? staffTabs.mentor
  const tab = useUI((s) => s.tab)
  const setTab = useUI((s) => s.setTab)
  const active = tabs.some((t) => t.id === tab) ? tab : tabs[0].id
  useEffect(() => {
    if (active !== tab) setTab(active)
  }, [active, tab, setTab])
  return (
    <main className="staff">
      <nav className="tabs" role="tablist">
        {tabs.map((t) => (
          <button key={t.id} role="tab" aria-selected={active === t.id} onClick={() => setTab(t.id)}>
            {t.label}
          </button>
        ))}
      </nav>
      <div className="staff-body">
        {active === 'questions' && <MentorInbox />}
        {active === 'team' && <TeamPanel />}
        {active === 'approvals' && <Approvals />}
        {active === 'insights' && <Insights />}
        {active === 'flags' && <FlagsPanel me={me} />}
        {active === 'chat' && (
          <div className="staff-chat">
            <Chat firstName={me.user.name.split(' ')[0]} />
          </div>
        )}
      </div>
    </main>
  )
}

function Toasts() {
  const toasts = useUI((s) => s.toasts)
  const dismiss = useUI((s) => s.dismiss)
  return (
    <div className="toasts" aria-live="polite">
      {toasts.map((t) => (
        <button key={t.id} className={`toast toast-${t.kind}`} onClick={() => dismiss(t.id)}>
          {t.text}
        </button>
      ))}
    </div>
  )
}
