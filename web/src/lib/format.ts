import type { Action } from '../api/schemas'

export const roleTitle: Record<string, string> = {
  intern: 'Стажёр',
  mentor: 'Наставник',
  lead: 'Руководитель',
  hrbp: 'HR BP',
}

export const taskStatus: Record<string, { label: string; tone: string }> = {
  todo: { label: 'К выполнению', tone: 'neutral' },
  in_progress: { label: 'В работе', tone: 'blue' },
  waiting: { label: 'Ждём', tone: 'purple' },
  blocked: { label: 'Заблокировано', tone: 'muted' },
  done: { label: 'Готово', tone: 'green' },
}

/** Human status of an action receipt. */
export function actionStatus(a: Action): { label: string; tone: string } {
  switch (a.status) {
    case 'planned':
      return { label: 'Ждёт подтверждения', tone: 'yellow' }
    case 'executing':
      return { label: 'Отправляется…', tone: 'blue' }
    case 'failed':
      return { label: 'Не отправлено', tone: 'red' }
    case 'cancelled':
      return { label: 'Отменено', tone: 'muted' }
  }
  switch (a.external_status) {
    case 'pending_approval':
      return { label: 'На согласовании', tone: 'purple' }
    case 'partially_granted':
      return { label: 'Частично выдано', tone: 'purple' }
    case 'approved':
      return { label: 'Согласовано', tone: 'green' }
    case 'granted':
      return { label: 'Выдано', tone: 'green' }
    case 'rejected':
      return { label: 'Отклонено', tone: 'red' }
    case 'completed':
      return { label: 'Отмечено', tone: 'green' }
    case 'open':
      return { label: 'Ждём ответ наставника', tone: 'purple' }
    case 'answered':
      return { label: 'Есть ответ', tone: 'green' }
  }
  return { label: 'Выполнено', tone: 'green' }
}

/** Offset between the (possibly simulated) server clock and the browser clock, ms. */
let clockOffset = 0
export function setServerNow(iso: string) {
  const t = new Date(iso).getTime()
  if (!Number.isNaN(t)) clockOffset = t - Date.now()
}

export function timeAgo(iso: string, now: Date = new Date(Date.now() + clockOffset)): string {
  const d = new Date(iso)
  const sec = Math.max(0, Math.round((now.getTime() - d.getTime()) / 1000))
  if (sec < 60) return 'только что'
  if (sec < 3600) return `${Math.floor(sec / 60)} мин назад`
  if (sec < 86400) return `${Math.floor(sec / 3600)} ч назад`
  // no Intl dependency: some runtimes (e.g. Alpine Node) ship without Russian ICU data
  return `${d.getDate()} ${monthsGen[d.getMonth()]}`
}

const monthsGen = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря']

/** Russian decimal format without Intl: 1.25 → "1,3", 33 → "33". */
export function ruNumber(v: number, digits = 1): string {
  const s = v.toFixed(digits).replace(/\.0+$/, '')
  return s.replace('.', ',')
}

export function hhmm(iso: string): string {
  return new Date(iso).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', timeZone: 'Europe/Moscow' })
}

/** Slash-commands expand to natural phrases understood by the buddy. */
export const slashCommands: Record<string, string> = {
  '/дальше': 'Что мне делать дальше?',
  '/отгул': 'Как оформить отгул?',
  '/доступы': 'Запроси стандартные доступы',
  '/наставник': 'Спроси наставника',
  '/расскажу': 'Хочу рассказать',
  '/заявки': 'Статус моих заявок',
}

export function expandSlash(text: string): string {
  const t = text.trim()
  const [cmd, ...rest] = t.split(/\s+/)
  const base = slashCommands[cmd.toLowerCase()]
  if (!base) return t
  return rest.length ? `${base}: ${rest.join(' ')}` : base
}
