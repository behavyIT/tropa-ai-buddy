import { expect, test, type Page } from '@playwright/test'
import { startScreencast } from './screencast'

/** Подпись поверх интерфейса — делает видео понятным без голоса. */
async function caption(page: Page, text: string, ms = 1800) {
  await page.evaluate((t) => {
    let el = document.getElementById('e2e-caption')
    if (!el) {
      el = document.createElement('div')
      el.id = 'e2e-caption'
      el.style.cssText =
        'position:fixed;left:50%;top:70px;transform:translateX(-50%);z-index:9999;background:#141414;color:#fff;padding:10px 18px;border-radius:12px;font:700 17px/1.35 system-ui;box-shadow:0 10px 30px rgba(0,0,0,.25);max-width:900px;text-align:center;pointer-events:none'
      document.body.appendChild(el)
    }
    el.textContent = t
  }, text)
  await page.waitForTimeout(ms)
}

async function say(page: Page, text: string) {
  const box = page.getByLabel('Сообщение')
  await box.click()
  await box.pressSequentially(text, { delay: 28 })
  await box.press('Enter')
  await expect(page.getByText('Тропа думает…')).toHaveCount(0, { timeout: 20000 })
  await page.waitForTimeout(900)
}

async function loginAs(page: Page, name: string) {
  const switchBtn = page.getByRole('button', { name: 'Сменить' })
  if (await switchBtn.isVisible().catch(() => false)) await switchBtn.click()
  await page.locator('button.persona', { hasText: name }).click()
}

test('путь стажёра: от запроса до результата', async ({ page, request }) => {
  // чистый стенд: день 1
  await request.post('/api/auth/login', { data: { login: 'alex', password: 'buddy2026' } })
  await request.post('/api/demo/reset', { headers: { 'X-Requested-With': 'tropa' } })
  await request.post('/api/auth/logout')

  await page.goto('/')
  const rec = await startScreencast(page, process.env.E2E_SCREENCAST_DIR)
  await caption(page, 'Тропа — AI-buddy для стажёров. Прототип для кейса Avito Start · данные синтетические', 2500)
  await loginAs(page, 'Алекс Новиков')
  await expect(page.getByText('Мой маршрут')).toBeVisible()
  await caption(page, '1. Тропа пишет первой: утренний бриф. Слева — маршрут по чек-листу Авито с зависимостями', 3200)

  await say(page, 'Что мне делать дальше?')
  await expect(page.getByText('Пока ждёшь — можно сделать').first()).toBeVisible()
  await caption(page, 'Следующий шаг с объяснением «почему», что ждём от наставника и что сделать пока', 3000)

  await caption(page, '2. Сценарий из кейса: «Как оформить отгул? Стоит ли отвлекать руководителя?»', 2000)
  await say(page, 'Как оформить отгул? Стоит ли отвлекать руководителя?')
  await page.locator('details.cite summary').last().click()
  await caption(page, 'Ответ по тестовой инструкции со ссылкой на источник — без выдумок', 2600)

  await say(page, 'Оформи отгул на 13 ноября')
  const plan = page.getByTestId('action-card').last()
  await expect(plan.getByText('Демо спринта', { exact: false })).toBeVisible()
  await caption(page, 'Тропа проверила календарь команды через MCP и подготовила заявку. Без подтверждения ничего не отправляется', 3200)
  await plan.getByRole('button', { name: 'Отправить заявку' }).click()
  await expect(plan.getByText('На согласовании')).toBeVisible()
  await caption(page, 'Заявка создана в сервисе отсутствий: квитанция с ID, статус «на согласовании»', 2800)

  await caption(page, '3. Доступы: стандартные — сразу, нестандартные — через руководителя, прод — запрещён ролью', 2400)
  await say(page, 'Запроси стандартные доступы')
  await page.getByTestId('action-card').last().getByRole('button', { name: 'Отправить заявки' }).click()
  await expect(page.getByTestId('action-card').last().getByText('Выдано')).toBeVisible()
  await caption(page, 'Доступы выданы — пункт маршрута закрылся и разблокировал avito-cli и stash', 2600)
  await say(page, 'Дай доступ к проду, хочу посмотреть данные')
  await caption(page, 'Права проверяет код, а не LLM: стажёру прод недоступен — Тропа объясняет и предлагает альтернативу', 3000)
  await say(page, 'Нужен доступ к k8s-staging')
  await page.getByTestId('action-card').last().getByRole('button', { name: 'Отправить заявки' }).click()
  await expect(page.getByTestId('action-card').last().getByText('На согласовании')).toBeVisible()

  await caption(page, '4. «Расскажи о Sentry» из чек-листа: стажёр объясняет — Тропа проверяет понимание по рубрике', 2600)
  await say(page, 'Хочу рассказать про Sentry')
  await say(
    page,
    'Sentry собирает ошибки и исключения из сервиса, показывает стектрейс и окружение. Одинаковые события группируются в issue, а алерты приходят в канал команды.',
  )
  await expect(page.getByText('Засчитано')).toBeVisible()
  await caption(page, 'Раскрыто 4 из 6 пунктов — подсказки к упущенному, пункт можно засчитать в чек-листе', 2600)
  await page.getByTestId('action-card').last().getByRole('button', { name: 'Отметить выполненным' }).click()
  await expect(page.getByTestId('action-card').last().getByText('Отмечено')).toBeVisible()

  await caption(page, '5. Вопроса нет в материалах → «Спросить без неловкости»: вопрос уйдёт наставнику в сводку', 2600)
  await say(page, 'Как называть ветки в монорепо фронтенда?')
  await page.getByTestId('action-card').last().getByRole('button', { name: 'Отправить вопрос' }).click()
  await expect(page.getByTestId('action-card').last().getByText('Ждём ответ наставника')).toBeVisible()
  await page.waitForTimeout(1200)

  await caption(page, '6. Руководитель: согласования выполняются в сервисах от её имени', 2200)
  await loginAs(page, 'Мария Соколова')
  await expect(page.getByRole('heading', { name: 'Согласования' })).toBeVisible()
  await page.waitForTimeout(1500)
  while ((await page.getByRole('button', { name: 'Согласовать' }).count()) > 0) {
    await page.getByRole('button', { name: 'Согласовать' }).first().click()
    await page.waitForTimeout(1200)
  }
  await page.getByRole('tab', { name: 'Тропинки' }).click()
  await caption(page, '«Тропинки»: где стажёры реально застревают, пробелы базы знаний и сэкономленные часы наставника', 3500)
  await page.mouse.wheel(0, 500)
  await page.waitForTimeout(1500)

  await caption(page, '7. Наставник: вопросы приходят сводкой, ответ сохраняется в базу знаний команды', 2200)
  await loginAs(page, 'Артём Ким')
  await page.getByLabel('Ответ').first().pressSequentially('Ветки называем по шаблону SRCH-123-short-name (синтетический ответ).', { delay: 18 })
  await page.getByRole('button', { name: 'Ответить' }).first().click()
  await expect(page.getByText('в базе знаний')).toBeVisible()
  await page.waitForTimeout(1200)

  await caption(page, '8. Стажёр получил ответ в чате, а следующий такой вопрос Тропа закроет сама — ответ один раз, навсегда', 2600)
  await loginAs(page, 'Алекс Новиков')
  await expect(page.getByText('Артём Ким ответил на твой вопрос', { exact: false }).first()).toBeVisible()
  await say(page, 'Как принято называть ветки в монорепо фронтенда?')
  await expect(page.getByText('уже отвечал на похожий вопрос', { exact: false }).last()).toBeVisible()
  await page.getByRole('button', { name: /Уведомления/ }).click()
  await caption(page, 'Уведомления: заявки согласованы, ответ наставника пришёл', 2600)
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: /Уведомления/ }).click()

  await caption(page, '9. Машина времени: следующее утро — новый бриф и напоминания', 2000)
  await page.getByRole('button', { name: /\+1 день/ }).click()
  await expect(page.getByText(/День 2 стажировки/).last()).toBeVisible()
  await page.waitForTimeout(2200)

  await caption(page, '10. Сбой корпоративного сервиса не ломает интерфейс: понятная ошибка и повтор', 2200)
  await page.getByRole('button', { name: 'Демо-меню' }).click()
  await page.getByRole('menuitem', { name: 'Имитировать сбой сервисов' }).click()
  await say(page, 'Нужен доступ к kafka')
  await page.getByTestId('action-card').last().getByRole('button', { name: 'Отправить заявки' }).click()
  await expect(page.getByTestId('action-card').last().getByText('Не отправлено')).toBeVisible()
  await page.waitForTimeout(1800)
  await page.getByRole('button', { name: 'Демо-меню' }).click()
  await page.getByRole('menuitem', { name: 'Выключить сбой сервисов' }).click()
  await page.waitForTimeout(800)
  await page.getByTestId('action-card').last().getByRole('button', { name: 'Повторить' }).click()
  await expect(page.getByTestId('action-card').last().getByText('На согласовании')).toBeVisible()
  await caption(page, 'Тропа: маршрут · ответы с учётом прав · действия через MCP · наставник без рутины', 3500)
  await rec.stop()
})
