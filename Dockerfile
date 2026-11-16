# syntax=docker/dockerfile:1.7
# ---------- 1. React UI ----------
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
# vite outDir = ../internal/webui/dist → /src/internal/webui/dist
RUN npm run build

# ---------- 2. Go binaries (UI is embedded via go:embed) ----------
FROM golang:1.26-alpine AS go
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN go build -ldflags="-s -w" -o /out/tropa ./cmd/tropa \
 && go build -ldflags="-s -w" -o /out/corp-mcp ./cmd/corp-mcp

# ---------- 3. Test runner (docker compose --profile test) ----------
FROM go AS test
# icu-data-full: русская локаль для Intl в тестах (по умолчанию в Alpine только en)
RUN apk add --no-cache nodejs npm icu-data-full
COPY --from=web /src/web/node_modules ./web/node_modules
CMD ["sh", "scripts/test-all.sh"]

# ---------- 4. Runtime ----------
FROM alpine:3.21 AS runtime
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app && mkdir -p /data && chown app /data
USER app
COPY --from=go /out/tropa /out/corp-mcp /usr/local/bin/
EXPOSE 8080 8090
ENTRYPOINT ["/usr/local/bin/tropa"]
