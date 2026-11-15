"""Builds coverage/summary.md from coverage/go.out and coverage/web/coverage-summary.json (after scripts/test-all.sh)."""
import collections, json, pathlib, sys

root = pathlib.Path(__file__).resolve().parent.parent
cov_dir = root / "coverage"
go_total = (cov_dir / "go-func.txt").read_text().strip().splitlines()[-1].split()[-1]
web = json.loads((cov_dir / "web" / "coverage-summary.json").read_text())["total"]
blocks = {}
for line in (cov_dir / "go.out").read_text().splitlines():
    if line.startswith("mode:"):
        continue
    key, n, c = line.rsplit(" ", 2)
    prev = blocks.get(key, (int(n), False))
    blocks[key] = (int(n), prev[1] or int(c) > 0)
tot, cov = collections.Counter(), collections.Counter()
for key, (n, c) in blocks.items():
    pkg = key.split(":")[0].rsplit("/", 1)[0]
    tot[pkg] += n
    cov[pkg] += n if c else 0
rows = "\n".join(f"| `{p}` | {100 * cov[p] / tot[p]:.1f}% | {cov[p]}/{tot[p]} |" for p in sorted(tot))
(cov_dir / "summary.md").write_text(f"""# Снимок отчёта о покрытии

Воспроизвести: `docker compose --profile test run --rm --build tests` или `sh scripts/test-all.sh` (нужен PostgreSQL).
Полные отчёты лежат рядом: `go.html`, `go-func.txt`, `web/index.html`, `web/lcov.info`, `web/coverage-summary.json`.

## Итог
| Часть | Покрытие | Инструмент |
|---|---|---|
| Go (весь прикладной код `cmd/…`, `internal/…`) | **{go_total}** инструкций (statements) | `go test -coverpkg=./internal/...,./cmd/... -coverprofile` + `go tool cover -func` |
| Web (`web/src`, без тестов) | **{web['lines']['pct']}%** строк, {web['branches']['pct']}% ветвей | Vitest + `@vitest/coverage-v8` |

Исключены только зависимости, сгенерированное и тестовый код (`*_test.go`, `*.test.ts(x)`, `web/src/test`).

## Go по пакетам
| Пакет | Покрытие | Инструкций |
|---|---|---|
{rows}
""", encoding="utf-8")
print("coverage/summary.md:", go_total, web["lines"]["pct"])
