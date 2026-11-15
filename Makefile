# Тропа — AI-buddy. Основные команды (см. README.md).
SHELL := /bin/sh
PG_DSN ?= postgres://localhost:5432/tropa?sslmode=disable

.PHONY: up down test-docker web build run test test-go test-web coverage e2e video fmt vet

up:            ## Поднять весь стенд в Docker: http://localhost:8080
	docker compose up --build -d && docker compose ps

down:
	docker compose down -v

test-docker:   ## Все тесты + отчёты покрытия в Docker (./coverage)
	docker compose --profile test run --rm --build tests

web:           ## Собрать React UI (встраивается в Go-бинарник)
	cd web && npm ci && npm run build

build: web     ## Собрать бинарники в ./bin
	CGO_ENABLED=0 go build -o bin/tropa ./cmd/tropa
	CGO_ENABLED=0 go build -o bin/corp-mcp ./cmd/corp-mcp

run: build     ## Локальный запуск одним процессом (корп. сервисы встроены, Redis не обязателен)
	DATABASE_URL="$(PG_DSN)" ./bin/tropa

test: test-go test-web

test-go:
	go test ./... -count=1

test-web:
	cd web && npx vitest run

coverage:      ## Отчёты покрытия (нужен PostgreSQL, см. TEST_DATABASE_URL)
	sh scripts/test-all.sh

e2e:           ## E2E-прогон основного сценария в браузере (нужен стенд на :8080 и Google Chrome)
	cd web && npx playwright test demo

video:         ## Записать видеодемо в docs/media/tropa-demo.mp4 (стенд на :8080, Chrome, ffmpeg)
	sh scripts/record-demo.sh

fmt:
	gofmt -w ./cmd ./internal ./content

vet:
	go vet ./...
