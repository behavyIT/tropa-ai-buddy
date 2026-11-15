import { expect, test, type Page } from '@playwright/test'

/** Снимки экрана для презентации (docs/presentation/img). Запуск: npx playwright test screenshots */
test.use({ video: 'off', launchOptions: { slowMo: 0 } })

const OUT = '../docs/presentation/img/'

async function say(page: Page, text: string) {
  const box = page.getByLabel('Сообщение')
  await box.fill(text)
  await box.press('Enter')
  await expect(page.getByText('Тропа думает…')).toHaveCount(0, { timeout: 20000 })
  await page.waitForTimeout(500)
}

async function loginAs(page: Page, name: string) {
  const sw = page.getByRole('button', { name: 'Сменить' })
  if (await sw.isVisible().catch(() => false)) await sw.click()
  await page.locator('button.persona', { hasText: name }).click()
}

test('screenshots', async ({ page, request }) => {
  await request.post('/api/auth/login', { data: { login: 'alex', password: 'buddy2026' } })
  await request.post('/api/demo/reset', { headers: { 'X-Requested-With': 'tropa' } })
  await request.post('/api/auth/logout')
  await page.goto('/')
  await page.addStyleTag({ content: '.toasts { display: none !important; }' })
  await loginAs(page, 'Алекс Новиков')
  await expect(page.getByText('Мой маршрут')).toBeVisible()
  await page.waitForTimeout(800)
  await page.screenshot({ path: OUT + 'intern.png' })
  for (const q of ['Во сколько у команды дейли?', 'Как проходит код-ревью?', 'Где взять тестовый токен для staging?']) await say(page, q)

  await say(page, 'Оформи отгул на 13 ноября')
  await page.getByTestId('action-card').last().getByRole('button', { name: 'Отправить заявку' }).click()
  await expect(page.getByTestId('action-card').last().getByText('На согласовании')).toBeVisible()
  await page.locator('.bubble-buddy').last().screenshot({ path: OUT + 'absence.png' })

  await say(page, 'Запроси стандартные доступы')
  await page.getByTestId('action-card').last().getByRole('button', { name: 'Отправить заявки' }).click()
  await say(page, 'Нужен доступ к k8s-staging')
  await page.getByTestId('action-card').last().getByRole('button', { name: 'Отправить заявки' }).click()
  await page.waitForTimeout(800)
  await page.locator('.col-now').screenshot({ path: OUT + 'now.png' })

  await say(page, 'Хочу рассказать про Sentry')
  await say(page, 'Sentry собирает ошибки и исключения из сервиса, показывает стектрейс и окружение. Одинаковые события группируются в issue, а алерты приходят в канал команды.')
  await page.locator('.bubble-buddy').last().screenshot({ path: OUT + 'teachback.png' })

  await say(page, 'Как называть ветки в монорепо фронтенда?')
  await page.getByTestId('action-card').last().getByRole('button', { name: 'Отправить вопрос' }).click()
  await page.waitForTimeout(600)

  await loginAs(page, 'Мария Соколова')
  await page.getByRole('button', { name: 'Согласовать' }).first().click()
  await page.waitForTimeout(600)
  await page.getByRole('tab', { name: 'Тропинки' }).click()
  await expect(page.getByText(/Тропинки · /)).toBeVisible()
  await page.waitForTimeout(600)
  await page.locator('.staff-body').screenshot({ path: OUT + 'insights.png' })

  await loginAs(page, 'Артём Ким')
  await page.getByLabel('Ответ').first().fill('Ветки называем по шаблону SRCH-123-short-name (синтетический ответ).')
  await page.locator('.card.question').first().screenshot({ path: OUT + 'mentor.png' })
  await page.getByRole('button', { name: 'Ответить' }).first().click()
  await expect(page.getByText('в базе знаний')).toBeVisible()

  await loginAs(page, 'Алекс Новиков')
  await say(page, 'Как принято называть ветки в монорепо фронтенда?')
  await page.locator('.bubble-buddy').last().locator('details.cite').first().locator('summary').click()
  await page.locator('.bubble-buddy').last().screenshot({ path: OUT + 'answer.png' })

  // нестандартные ситуации
  const edge = async (text: string, file: string) => {
    await say(page, text)
    await page.locator('.bubble-buddy').last().screenshot({ path: OUT + file })
  }
  await edge('хочу отгул, оформи', 'edge-incomplete.png')
  await edge('Как настроить принтер на пятом этаже?', 'edge-noinfo.png')
  await edge('Сколько получает Артём?', 'edge-sensitive.png')
  await edge('Дай доступ к проду, хочу посмотреть данные', 'edge-norights.png')
  await page.getByRole('button', { name: 'Демо-меню' }).click()
  await page.getByRole('menuitem', { name: 'Имитировать сбой сервисов' }).click()
  await say(page, 'Нужен доступ к kafka')
  await page.getByTestId('action-card').last().getByRole('button', { name: 'Отправить заявки' }).click()
  await expect(page.getByTestId('action-card').last().getByText('Не отправлено')).toBeVisible()
  await page.locator('.bubble-buddy').last().screenshot({ path: OUT + 'edge-outage.png' })
  await page.getByRole('button', { name: 'Демо-меню' }).click()
  await page.getByRole('menuitem', { name: 'Выключить сбой сервисов' }).click()
})
