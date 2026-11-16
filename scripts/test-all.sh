#!/bin/sh
# Запускает все автотесты и формирует воспроизводимые отчёты покрытия в ./coverage.
# Требует PostgreSQL: TEST_DATABASE_URL (по умолчанию postgres://localhost:5432/tropa_test).
set -eu
cd "$(dirname "$0")/.."
mkdir -p coverage
export CGO_ENABLED=0

echo "== Go: unit + integration tests (coverage across all application packages)"
go test ./... -count=1 -coverpkg=./internal/...,./cmd/... -coverprofile=coverage/go.out
go tool cover -func=coverage/go.out > coverage/go-func.txt
go tool cover -html=coverage/go.out -o coverage/go.html
GO_TOTAL=$(tail -1 coverage/go-func.txt | awk '{print $NF}' | tr -d '%')
echo "Go line coverage: ${GO_TOTAL}%"

echo "== Web: vitest + coverage (v8)"
(cd web && npx vitest run --coverage)
# отчёт без абсолютных путей машины, на которой запускали тесты
sed -i.bak 's#"/[^"]*/web/src/#"web/src/#g' coverage/web/coverage-summary.json && rm -f coverage/web/coverage-summary.json.bak

awk -v t="$GO_TOTAL" 'BEGIN { if (t+0 < 80) { print "Go coverage below 80%"; exit 1 } }'
echo "OK: all tests passed, reports in ./coverage (go.html, go-func.txt, web/index.html)"
