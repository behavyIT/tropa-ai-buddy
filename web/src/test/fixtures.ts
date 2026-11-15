export const person = (id: string, name: string, role: string, team = 'ads-backend') => ({
  id,
  name,
  role,
  title: role === 'intern' ? 'Стажёр Backend' : 'Сотрудник',
  team_id: team,
  team_name: 'Объявления: Backend',
  contact: '@' + id,
})

export const demo = { enabled: true, now: '2026-11-09T09:30:00+03:00', today: 'пн, 9 ноября', day: 1, corp_fault: false }

export const flags = [
  { key: 'actions.absence', description: 'Оформление отгула', default: true, enabled: true, source: 'default' },
  { key: 'teachback', description: 'Режим «Расскажи мне»', default: true, enabled: false, source: 'team' },
]

export const me = (role = 'intern') => ({
  user: person(role === 'intern' ? 'u-intern-alex' : 'u-' + role, role === 'intern' ? 'Алекс Новиков' : 'Мария Соколова', role),
  mentor: role === 'intern' ? person('u-mentor-artem', 'Артём Ким', 'mentor') : undefined,
  flags,
  demo,
  llm: 'offline',
})

const task = (id: string, extra: Record<string, unknown> = {}) => ({
  id,
  title: 'Задача ' + id,
  owner: 'intern',
  owner_name: 'Алекс Новиков',
  kind: 'manual',
  status: 'todo',
  due_day: 1,
  due_date: 'пн, 9 ноября',
  overdue: false,
  blocked_by: null,
  can_complete: false,
  can_nudge: false,
  can_start: false,
  systems: null,
  ...extra,
})

export const route = {
  user_id: 'u-intern-alex',
  user_name: 'Алекс Новиков',
  route_title: 'Маршрут Backend-стажёра',
  day: 1,
  today: 'пн, 9 ноября',
  done: 1,
  total: 6,
  percent: 16,
  overdue: 1,
  stages: [
    {
      id: 'start',
      title: 'Первые дни — знакомство',
      from_day: 1,
      to_day: 3,
      done: 1,
      total: 4,
      current: true,
      tasks: [
        task('meet-mentor', { can_start: true, can_complete: true, doc: 'communication', doc_title: 'Как и кого спрашивать', overdue: true }),
        task('add-to-chats', { owner: 'mentor', owner_name: 'Артём Ким', can_nudge: true }),
        task('teach-sentry', { title: 'Расскажи о Sentry', kind: 'teachback', topic: 'sentry' }),
        task('access-standard', { kind: 'access', systems: ['gitlab', 'jira'] }),
      ],
    },
    {
      id: 'week1',
      title: 'Неделя 1',
      from_day: 1,
      to_day: 7,
      done: 1,
      total: 2,
      current: false,
      tasks: [task('done-one', { status: 'done' }), task('blocked', { status: 'blocked', waiting_for: 'Ждёт: Артём Ким', blocked_by: ['x'] })],
    },
  ],
  next: [
    { task_id: 'meet-mentor', title: 'Познакомься с наставником', status: 'todo', why: 'Срок — сегодня', due: 'пн, 9 ноября' },
    { task_id: 'access-standard', title: 'Запроси доступы', status: 'todo', why: 'откроет 3', due: 'вт' },
  ],
  waiting: [{ task_id: 'blocked', title: 'Заблокированная', status: 'blocked', why: 'Ждёт: Артём Ким', due: 'ср' }],
  while_waiting: [{ task_id: 'notes', title: 'Конспект', status: 'todo', why: '', due: 'вт' }],
}

export const action = (extra: Record<string, unknown> = {}) => ({
  id: 'a1',
  type: 'absence.create',
  status: 'planned',
  title: 'Отгул: пт, 13 ноября',
  view: {
    service: 'Сервис отсутствий',
    fields: [
      { label: 'Даты', value: 'пт, 13 ноября' },
      { label: 'Комментарий', value: 'Личные дела' },
    ],
    warnings: ['Демо спринта в 16:00'],
    confirm_label: 'Отправить заявку',
    approver: 'Мария Соколова',
    permission: 'Только за себя',
    note: 'Заметка',
  },
  created_at: '2026-11-09T09:31:00+03:00',
  updated_at: '2026-11-09T09:31:00+03:00',
  ...extra,
})

export const buddyMessage = (extra: Record<string, unknown> = {}) => ({
  id: 'm2',
  role: 'buddy',
  text: 'Вот ответ [1].\n\nВторой абзац\nстрока',
  payload: {
    intent: 'question',
    mode: 'llm',
    citations: [
      { n: 1, doc_id: 'd1', title: 'Документ', kind: 'test_instruction', snippet: 'Текст' },
      { n: 2, doc_id: 'card:1', title: 'Карточка', kind: 'mentor_answer', snippet: 'Ответ', author: 'Артём Ким' },
    ],
    cards: [
      { type: 'action', action_id: 'a1' },
      { type: 'steps', title: 'Следующий шаг', steps: [{ task_id: 't', title: 'Шаг один', status: 'todo', why: 'потому что', due: '' }] },
      { type: 'person', person: { id: 'p', name: 'Артём Ким', title: 'Наставник', contact: '@artem', topic: 'Код', how: 'Пиши', channel: '#dev' } },
      {
        type: 'teachback',
        teachback: { topic: 'sentry', title: 'Sentry', score: 66, passed: true, covered: [{ id: 'e', label: 'Ошибки' }], missing: [{ id: 'r', label: 'Релизы', hint: 'Подсказка' }], doc_id: 'sentry' },
      },
      { type: 'requests', action_ids: ['a1'] },
    ],
    suggestions: ['Что мне делать дальше?'],
  },
  feedback: 0,
  created_at: '2026-11-09T09:31:00+03:00',
  ...extra,
})

export const userMessage = { id: 'm1', role: 'user', text: 'Вопрос', payload: { citations: null, cards: null, suggestions: null }, feedback: 0, created_at: '2026-11-09T09:30:00+03:00' }

export const insights = {
  scope: 'Команда «Объявления: Backend»',
  interns: [{ id: 'u1', name: 'Алекс', team: 'ads', day: 2, done: 3, total: 22, percent: 13, overdue: 1, waiting: ['k8s'], next: 'Шаг' }],
  metrics: {
    chat_turns: 10, answered: 7, unanswered: 3, self_serve_rate: 70, helpful: 2, not_helpful: 1, escalated: 2, merged: 1, mentor_answers: 1, published: 1,
    actions_executed: 4, actions_failed: 1, teachbacks_passed: 1, tasks_done: 3, permission_denied: 1, sensitive_blocked: 0, mentor_minutes_saved: 95, llm_share: 0, avg_access_wait_hours: 33.3,
  },
  hotspots: [{ task_id: 'access-k8s', title: 'Доступ к k8s', questions: 9, waiting: 1, overdue: 0, advice: 'Автовыдача' }],
  gaps: [{ topic: 'staging токен', count: 2, source: 'unanswered', status: 'нет в базе' }],
  assumptions: ['Допущение'],
  synthetic_events: 40,
}
