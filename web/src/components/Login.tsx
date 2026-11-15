import { useState } from 'react'
import { useLogin } from '../api/hooks'

const personas = [
  { login: 'alex', name: 'Алекс Новиков', role: 'Стажёр Backend', hint: 'Основной сценарий: маршрут, чат, заявки', color: 'mint' },
  { login: 'dana', name: 'Дана Орлова', role: 'Стажёр Frontend', hint: 'Другая команда — другие материалы и права', color: 'sky' },
  { login: 'artem', name: 'Артём Ким', role: 'Наставник', hint: 'Сводка вопросов, ответы в базу знаний', color: 'lilac' },
  { login: 'maria', name: 'Мария Соколова', role: 'Руководитель', hint: 'Согласования, «Тропинки», флаги', color: 'pink' },
  { login: 'olga', name: 'Ольга Петрова', role: 'HR BP', hint: 'Аналитика по всем командам', color: 'mint' },
]

export const TEST_PASSWORD = 'buddy2026'

export function Login() {
  const login = useLogin()
  const [form, setForm] = useState({ login: '', password: '' })
  const go = (l: string, p: string) => login.mutate({ login: l, password: p })
  return (
    <main className="login">
      <section className="login-hero">
        <div className="logo-big">
          Тропа<span className="logo-tag">AI-buddy</span>
        </div>
        <h1>
          <span className="hl hl-green">Проводник</span> по первым неделям стажировки
        </h1>
        <p className="muted">
          Маршрут адаптации с зависимостями, ответы с учётом прав, заявки в корпоративные сервисы в один клик — и наставник,
          которого не дёргают по пустякам.
        </p>
        <p className="disclaimer">
          Прототип для кейса Hack&amp;Change 2026. Все данные синтетические, инструкции — тестовые и не являются регламентами Авито.
        </p>
      </section>
      <section className="login-cards" aria-label="Тестовые пользователи">
        <h2>Войти как</h2>
        <div className="persona-grid">
          {personas.map((p) => (
            <button key={p.login} className={`persona sticker-${p.color}`} onClick={() => go(p.login, TEST_PASSWORD)} disabled={login.isPending}>
              <strong>{p.name}</strong>
              <span className="persona-role">{p.role}</span>
              <span className="persona-hint">{p.hint}</span>
            </button>
          ))}
        </div>
        <form
          className="login-form"
          onSubmit={(e) => {
            e.preventDefault()
            go(form.login, form.password)
          }}
        >
          <label>
            Логин
            <input value={form.login} onChange={(e) => setForm({ ...form, login: e.target.value })} autoComplete="username" />
          </label>
          <label>
            Пароль
            <input type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} autoComplete="current-password" />
          </label>
          <button className="btn btn-primary" type="submit" disabled={login.isPending}>
            Войти
          </button>
        </form>
        <p className="muted small">
          Тестовые логины: alex, dana, artem, maria, olga · пароль <code>{TEST_PASSWORD}</code>
        </p>
        {login.isError && (
          <div className="errorbox" role="alert">
            {login.error.message}
          </div>
        )}
      </section>
    </main>
  )
}
