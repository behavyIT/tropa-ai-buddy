# Тропа — AI-buddy для стажёров

> Кейс Avito Start · Hack&Change 2026. Прототип ИИ-наставника, который ведёт стажёра по маршруту адаптации,
> отвечает с учётом команды и прав, оформляет заявки в корпоративных сервисах (через MCP) только после
> подтверждения — и забирает у наставника рутину, не подменяя его.

[![CI](https://github.com/behavyIT/tropa-ai-buddy/actions/workflows/ci.yml/badge.svg)](https://github.com/behavyIT/tropa-ai-buddy/actions/workflows/ci.yml)

| Что сдаём | Где |
|---|---|
| Работающий прототип с интерфейсом и исходный код | этот репозиторий · запуск — `docker compose up --build` ([ниже](#быстрый-старт-docker)) |
| Инструкция запуска, тестовые данные, порядок проверки | [этот README](#тестовые-учётные-данные) |
| SDD-спецификации | [`openspec/specs`](openspec/specs) · [трассируемость спек → тестов](openspec/TRACEABILITY.md) |
| Тесты и отчёт о покрытии (> 80%) | Go **89,0%**, Web **99,8%** — [coverage/summary.md](coverage/summary.md), [go.html](coverage/go.html), [web/index.html](coverage/web/index.html) |
| Замеры качества | [docs/eval](docs/eval/README.md): 49/49 интентов, 16/16 ответов из верного источника, 10/10 честных «не знаю», отклик < 1 мс |
| 📹 Видеодемонстрация (1:50) | [`docs/media/tropa-demo.mp4`](docs/media/tropa-demo.mp4) — путь от запроса до результата |
| 📊 Продуктовая презентация (PDF) | [`docs/presentation/Tropa-AI-buddy.pdf`](docs/presentation/Tropa-AI-buddy.pdf) |

Все данные синтетические, все инструкции — **тестовые и не являются регламентами Авито**.

---

## Что внутри (коротко)

| | |
|---|---|
| **Маршрут вместо чек-листа** | Чек-лист стажёра Авито → граф задач с зависимостями. «Следующий лучший шаг» с объяснением *почему*, «Ждём …» и «Пока ждёшь — сделай». |
| **Ответы с учётом прав** | BM25-поиск по материалам команды; ACL применяется *до* ранжирования — недоступное не попадает ни в ответ, ни в LLM. Ссылки [n] на источники с пометкой «Тестовая инструкция» / «Ответ наставника». |
| **Действия через MCP** | Отгул (сервис отсутствий + проверка календаря команды), доступы (автовыдача / согласование / запрет по роли), отметки чек-листа. Всегда «план → подтверждение → квитанция», exactly-once. |
| **Спросить без неловкости** | Вопрос наставнику уходит в сводку, похожие объединяются, срочные — сразу. Ответ наставника сохраняется в базу: **ответ один раз — навсегда**. Пункты наставника Тропа напоминает сама. |
| **«Расскажи мне»** | Пункты чек-листа «Расскажи о Sentry/Grafana»: Тропа слушает объяснение, проверяет по рубрике, подсказывает упущенное и засчитывает пункт. |
| **Проактивность (ИИ-крон)** | Утренний бриф, напоминания о просрочках, авто-напоминания наставнику/руководителю, синхронизация статусов, сводка вопросов. |
| **«Тропинки»** | Для руководителя и HR BP: где стажёры реально застревают, пробелы базы знаний, сэкономленные часы наставника. |
| **LLM — опционально** | Без ключа всё работает (детерминированный русский NLU). С ключом — Claude извлекает интент по JSON-схеме, формулирует ответ строго по источникам и оценивает teach-back; при сбое — офлайн-fallback. |

Подробно: [архитектура](docs/architecture.md) · [путь стажёра и выбор сценариев](docs/user-journey.md) ·
[метрики и эксперимент](docs/metrics.md) · [безопасность](docs/security.md) · [сценарий демо](docs/demo-script.md) ·
[SDD-спецификации](openspec/) · [трассируемость спек → тестов](openspec/TRACEABILITY.md).

---

## Быстрый старт (Docker)

Требуется Docker с Compose v2.

```bash
git clone https://github.com/behavyIT/tropa-ai-buddy && cd tropa-ai-buddy
```
```bash
docker compose up --build
```

Откройте **http://localhost:8080**. Поднимаются: PostgreSQL 16, Redis 7, `corp-mcp` (имитация корпоративных
сервисов Авито по MCP, порт 8090) и `tropa` (API + встроенный UI, порт 8080).

### Тестовые учётные данные

Пароль у всех: **`buddy2026`** (на экране входа можно просто нажать на карточку).

| Логин | Кто | Что смотреть |
|---|---|---|
| `alex` | Алекс Новиков, стажёр Backend («Объявления: Backend») | основной сценарий |
| `dana` | Дана Орлова, стажёр Frontend («Поиск: Frontend») | другая команда — другие материалы и права |
| `artem` | Артём Ким, наставник Алекса | сводка вопросов, ответы в базу знаний, маршрут подопечного |
| `maria` | Мария Соколова, руководитель «Объявления: Backend» | согласования, «Тропинки», флаги команды |
| `olga` | Ольга Петрова, HR BP | аналитика по всем командам, глобальные флаги |

### Порядок проверки

1. Войти как **alex** → в чате уже утренний бриф. Нажать «Что мне делать дальше?».
2. «Как оформить отгул? Стоит ли отвлекать руководителя?» → затем «Оформи отгул на 13 ноября» → «Отправить заявку» (квитанция `ABS-…`).
3. «Запроси стандартные доступы» → подтвердить; «Дай доступ к проду» (отказ по правам); «Нужен доступ к k8s-staging».
4. «Хочу рассказать про Sentry» → объяснить в 3–4 предложениях → «Отметить выполненным».
5. «Как называть ветки в монорепо фронтенда?» → «Отправить вопрос» наставнику.
6. «Сменить» → **maria** → «Согласования» → согласовать; «Тропинки».
7. «Сменить» → **artem** → «Сводка вопросов» → ответить с галочкой «Сохранить в базу знаний».
8. Снова **alex** → ответ наставника в чате; повторить вопрос — ответ из базы. **⏩ +1 день** — новый бриф и напоминания.
   **⋯ → «Имитировать сбой сервисов»** → заявка получает статус «Не отправлено» и кнопку «Повторить».

Полный сценарий — [docs/demo-script.md](docs/demo-script.md). Сбросить состояние: ⋯ → «Сбросить демо».

**E2E и видео.** Сценарий выше автоматизирован в Playwright (`web/e2e/demo.spec.ts`, Google Chrome): `make e2e` проверяет путь
целиком, `make video` записывает его в `docs/media/tropa-demo.mp4` (кадры через Chrome DevTools Protocol + ffmpeg).

---

## Тесты и покрытие

```bash
docker compose --profile test run --rm --build tests
```

Команда прогоняет все Go-тесты (unit + интеграционные на реальном PostgreSQL, каждая проверка — в своей схеме)
и Vitest, формирует отчёты в `./coverage`:

| Отчёт | Файл |
|---|---|
| Go, HTML | `coverage/go.html` |
| Go, построчно по функциям | `coverage/go-func.txt` (итог — последняя строка) |
| Web (v8), HTML / LCOV / JSON | `coverage/web/index.html`, `coverage/web/lcov.info`, `coverage/web/coverage-summary.json` |

Покрытие строк прикладного кода (без зависимостей, сгенерированного кода и самих тестов):
**Go — 89,0%** (statement coverage по `go tool cover`), **Web — 99,8%** (порог 80% проверяется в `scripts/test-all.sh` и в `vite.config.ts`).
Снимок отчёта на момент сдачи — [coverage/summary.md](coverage/summary.md).

Без Docker (нужен PostgreSQL с базой `tropa_test`):
```bash
export TEST_DATABASE_URL="postgres://localhost:5432/tropa_test?sslmode=disable"
```
```bash
sh scripts/test-all.sh
```

**Замеры качества** ([docs/eval](docs/eval/README.md)) — те же тесты с записью отчётов:
```bash
EVAL_REPORT=1 go test ./internal/nlu ./internal/app -run EVAL -count=1
```
Время отклика по запущенному стенду: `python3 scripts/eval/latency.py`.

**CI.** GitHub Actions на каждый push: `go vet`, все тесты с отчётами покрытия (артефакт `coverage`), сборка Docker-образов,
запуск стенда через `docker compose`, проверка `/readyz` и сквозной e2e-прогон в браузере по поднятому стенду.

**Спецификации SDD.** Поведение агента и инструментов описано в [`openspec/specs`](openspec/specs) (OpenSpec):
105 сценариев с ID (`[ABS-02]`, `[QA-02]`, `[EVAL-02]`, …). Тест `internal/spectrace` проверяет, что у каждого сценария есть
автотест (`TestABS02_…` в Go или `[UI-07] …` в Vitest), — сценарий без теста роняет сборку.

---

## LLM (внешний API) — опционально

| Параметр | Значение |
|---|---|
| Сервис | Anthropic Claude API (`https://api.anthropic.com`), официальный Go SDK `anthropic-sdk-go/v2` |
| Модель | `claude-opus-5-5` (переменная `LLM_MODEL`), `LLM_EFFORT=low` для быстрого отклика |
| Включение | `LLM_PROVIDER=anthropic` |
| Ключ | `ANTHROPIC_API_KEY` — только через окружение, в репозитории ключей нет |
| Прокси | `ANTHROPIC_BASE_URL` (необязательно) |

```bash
LLM_PROVIDER=anthropic ANTHROPIC_API_KEY=sk-... docker compose up --build
```

Что делает модель: извлекает интент/слоты по JSON-схеме (structured outputs), пишет ответ только по переданным
источникам со ссылками [n] (или `НЕТ_ОТВЕТА`), оценивает teach-back по рубрике. Права и действия решает код.
Ошибка, таймаут или отказ модели → ответ офлайн-логикой (пометка «LLM недоступна → офлайн»). Пример конфигурации —
[`.env.example`](.env.example).

---

## Локальный запуск без Docker

Нужны Go 1.26+, Node 22+, PostgreSQL 15+ (Redis не обязателен: без `REDIS_URL` лимиты работают в памяти;
без `CORP_MCP_URL` корпоративные сервисы встроены в процесс и вызываются по тому же MCP-протоколу).

```bash
cd web && npm ci && npm run build && cd ..
```
```bash
DATABASE_URL="postgres://postgres:postgres@localhost:5432/tropa?sslmode=disable" go run ./cmd/tropa
```

Отдельный MCP-сервер корпоративных сервисов: `go run ./cmd/corp-mcp` (порт 8090), затем
`CORP_MCP_URL=http://localhost:8090/mcp` для `tropa`. Dev-режим UI: `cd web && npm run dev` (прокси `/api` → :8080).

---

## Версии зависимостей

| Компонент | Версия |
|---|---|
| Go | 1.26 |
| PostgreSQL / Redis (Docker) | 16-alpine / 7-alpine |
| pgx | v5.12.0 |
| go-redis | v9.23.0 |
| anthropic-sdk-go | v2.7.0 |
| golang-jwt | v5.3.1 |
| prometheus/client_golang | v1.25.0 |
| miniredis (тесты) | v2.40.0 |
| Node (сборка) | 22 |
| React / React DOM | 19.1.0 |
| @tanstack/react-query | 5.80.7 |
| Zustand | 5.0.5 |
| Zod | 3.25.67 |
| Vite / Vitest / @vitest/coverage-v8 | 6.3.5 / 3.2.4 / 3.2.4 |
| TypeScript | 5.8.3 |
| Testing Library (react / user-event / jest-dom) | 16.3.0 / 14.6.1 / 6.6.3 |
| Playwright (видео-e2e) | 1.53.1 |

Точные версии зафиксированы в `go.sum` и `web/package-lock.json`.

---

## Конфигурация

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `HTTP_ADDR` | `:8080` | адрес API/UI |
| `DATABASE_URL` | — | PostgreSQL |
| `REDIS_URL` | пусто → память | лимиты, лидер крона |
| `CORP_MCP_URL` | пусто → встроенный | MCP-сервер корпоративных сервисов |
| `CORP_TOKEN` | `dev-corp-token` | сервисный токен Тропа → MCP |
| `SESSION_SECRET` | демо-значение | подпись сессий (обязателен при `DEMO_MODE=false`) |
| `DEMO_MODE` | `true` | машина времени, сброс, имитация сбоя |
| `SIM_START` | `2026-11-09T09:30:00+03:00` | «первый день» стажировки |
| `CORP_LATENCY` | `350ms` | имитация задержки сервисов |
| `LLM_PROVIDER` / `ANTHROPIC_API_KEY` / `LLM_MODEL` / `LLM_EFFORT` | `none` / — / `claude-opus-5-5` / `low` | LLM |
| `CRON_INTERVAL` | `20s` | период ИИ-крона |

---

## Структура

```
cmd/tropa            API + встроенный UI          cmd/corp-mcp       MCP-сервер имитации сервисов
internal/agent       оркестратор агента            internal/nlu       офлайн-понимание русского
internal/llm         Claude: схемы, промпты         internal/kb        база знаний (BM25 + ACL)
internal/route       маршрут, next best step        internal/actions   план → подтверждение → квитанция
internal/mentor      сводка вопросов, база знаний   internal/cron      проактивные задачи (ИИ-крон)
internal/policy      права                          internal/mcp       протокол MCP, клиент с breaker
internal/corpmock    сервисы отсутствий/доступов/чек-листа/календаря
internal/insights    «Тропинки»                     internal/flags     фича-флаги
content/             маршруты (из чек-листа Авито), командные паки, тестовые инструкции, рубрики, каталог доступов
openspec/            SDD-спецификации, трассируемость, архив изменения
web/                 React UI (+ Vitest, Playwright)
docs/                архитектура, метрики, безопасность, сценарий демо, медиа, презентация
```

API: `/api/me`, `/api/route`, `/api/chat`, `/api/actions/{id}/confirm|cancel`, `/api/approvals`, `/api/mentor/questions`,
`/api/insights`, `/api/flags`, `/api/demo/*`, `/healthz`, `/readyz`, `/metrics`.
