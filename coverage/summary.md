# Снимок отчёта о покрытии

Воспроизвести: `docker compose --profile test run --rm --build tests` или `sh scripts/test-all.sh` (нужен PostgreSQL).
Полные отчёты лежат рядом: `go.html`, `go-func.txt`, `web/index.html`, `web/lcov.info`, `web/coverage-summary.json`.

## Итог
| Часть | Покрытие | Инструмент |
|---|---|---|
| Go (весь прикладной код `cmd/…`, `internal/…`) | **89.0%** инструкций (statements) | `go test -coverpkg=./internal/...,./cmd/... -coverprofile` + `go tool cover -func` |
| Web (`web/src`, без тестов) | **99.83%** строк, 94.77% ветвей | Vitest + `@vitest/coverage-v8` |

Исключены только зависимости, сгенерированное и тестовый код (`*_test.go`, `*.test.ts(x)`, `web/src/test`).

## Go по пакетам
| Пакет | Покрытие | Инструкций |
|---|---|---|
| `tropa/cmd/corp-mcp` | 69.7% | 23/33 |
| `tropa/cmd/tropa` | 53.8% | 7/13 |
| `tropa/internal/actions` | 83.6% | 357/427 |
| `tropa/internal/agent` | 85.0% | 499/587 |
| `tropa/internal/app` | 91.3% | 147/161 |
| `tropa/internal/apperr` | 90.9% | 10/11 |
| `tropa/internal/auth` | 85.3% | 29/34 |
| `tropa/internal/catalog` | 98.4% | 123/125 |
| `tropa/internal/clock` | 100.0% | 32/32 |
| `tropa/internal/corp` | 96.3% | 26/27 |
| `tropa/internal/corpmock` | 94.6% | 298/315 |
| `tropa/internal/cron` | 90.4% | 75/83 |
| `tropa/internal/directory` | 92.3% | 36/39 |
| `tropa/internal/flags` | 81.8% | 27/33 |
| `tropa/internal/httpapi` | 88.9% | 319/359 |
| `tropa/internal/insights` | 84.1% | 138/164 |
| `tropa/internal/kb` | 97.1% | 100/103 |
| `tropa/internal/llm` | 93.3% | 70/75 |
| `tropa/internal/mcp` | 96.3% | 183/190 |
| `tropa/internal/mentor` | 84.4% | 65/77 |
| `tropa/internal/nlu` | 98.2% | 109/111 |
| `tropa/internal/notify` | 72.7% | 8/11 |
| `tropa/internal/policy` | 87.2% | 41/47 |
| `tropa/internal/ratelimit` | 97.2% | 35/36 |
| `tropa/internal/route` | 95.0% | 151/159 |
| `tropa/internal/seed` | 82.1% | 23/28 |
| `tropa/internal/store` | 84.6% | 236/279 |
| `tropa/internal/store/storetest` | 79.2% | 19/24 |
| `tropa/internal/textutil` | 100.0% | 49/49 |
| `tropa/internal/webui` | 81.0% | 17/21 |
