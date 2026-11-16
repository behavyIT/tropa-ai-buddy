package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"tropa/content"
	"tropa/internal/app"
	"tropa/internal/catalog"
	"tropa/internal/corpmock"
	"tropa/internal/directory"
	"tropa/internal/store/storetest"
)

func TestAPP02_LoadConfigFromEnv(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9999")
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("SECURE_COOKIE", "notabool")
	t.Setenv("CRON_INTERVAL", "5s")
	c, err := app.LoadConfig()
	if err != nil || c.Addr != ":9999" || c.CronInterval != 5*time.Second || c.SecureCookie || c.SessionSecret == "" {
		t.Fatalf("config: %+v %v", c, err)
	}
	t.Setenv("CRON_INTERVAL", "x")
	if _, err := app.LoadConfig(); err == nil {
		t.Fatal("bad cron interval")
	}
	t.Setenv("CRON_INTERVAL", "5s")
	t.Setenv("CORP_LATENCY", "x")
	if _, err := app.LoadConfig(); err == nil {
		t.Fatal("bad latency")
	}
	t.Setenv("CORP_LATENCY", "1ms")
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("SESSION_SECRET", "")
	if _, err := app.LoadConfig(); err == nil {
		t.Fatal("prod mode requires secret")
	}
	t.Setenv("SESSION_SECRET", "s")
	t.Setenv("LLM_PROVIDER", "anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "")
	if _, err := app.LoadConfig(); err == nil {
		t.Fatal("anthropic requires key")
	}
}

// TestAPP03_ExternalCorpAndRedis runs the production topology: the corporate
// MCP server is a separate HTTP service and Redis backs rate limits/locks.
func TestAPP03_ExternalCorpAndRedis(t *testing.T) {
	cat, _ := catalog.Load(content.FS)
	mock := corpmock.New(directory.New(), cat, "ext", nil)
	corpSrv := httptest.NewServer(mock.Handler("ext"))
	defer corpSrv.Close()
	mr := miniredis.RunT(t)
	h := newHarness(t, func(c *app.Config) {
		c.CorpMCPURL, c.CorpToken, c.RedisURL = corpSrv.URL+"/mcp", "ext", "redis://"+mr.Addr()
		// a simulated start far in the past: without a shared demo clock the external
		// service (real wall clock) would reject the 13 Nov day off as "in the past"
		c.SimStart = time.Date(2020, 11, 9, 9, 30, 0, 0, time.FixedZone("MSK", 3*3600))
	})
	alex := h.login("alex")
	a := alex.chat("Запроси стандартные доступы").action(t)
	if d := alex.confirm(a.ID); d.ExternalStatus != "granted" {
		t.Fatalf("external corp: %+v", d)
	}
	// the external service must follow the demo time machine, not its own wall clock
	ab := alex.chat("Оформи отгул на 13 ноября").action(t)
	if d := alex.confirm(ab.ID); d.ExternalStatus != "pending_approval" {
		t.Fatalf("external corp must accept a date that is in the future of the simulated clock: %+v", d)
	}
	if code := alex.post("/api/demo/corp-fault", map[string]bool{"enabled": true}, &map[string]any{}); code != 200 || !mock.Fault() {
		t.Fatalf("remote fault: %d", code)
	}
	resp, _ := http.Get(h.srv.URL + "/readyz")
	if resp.StatusCode != 503 {
		t.Fatalf("readyz during outage: %d", resp.StatusCode)
	}
	alex.post("/api/demo/corp-fault", map[string]bool{"enabled": false}, &map[string]any{})
	if code := alex.post("/api/demo/reset", nil, &map[string]any{}); code != 200 {
		t.Fatalf("reset: %d", code)
	}
	corpSrv.Close()
	if code := alex.post("/api/demo/corp-fault", map[string]bool{"enabled": true}, &map[string]any{}); code != 503 {
		t.Fatalf("fault toggle with corp down: %d", code)
	}
	h.app.Close()
}

func TestAPP04_RunServesAndShutsDown(t *testing.T) {
	cfg := testConfig()
	cfg.Addr = "127.0.0.1:18393"
	cfg.DisableCron = false
	cfg.CronInterval = 10 * time.Millisecond
	a, err := app.New(context.Background(), cfg, storetest.New(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	time.Sleep(150 * time.Millisecond)
	resp, err := http.Get("http://127.0.0.1:18393/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("run: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if l := app.NewLogger("debug"); l == nil {
		t.Fatal("logger")
	}
	bad := testConfig()
	bad.RedisURL = "::bad"
	if _, err := app.New(context.Background(), bad, storetest.New(t), slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Fatal("bad redis url")
	}
	nodb := testConfig()
	nodb.DatabaseURL = "postgres://nobody@127.0.0.1:1/x?sslmode=disable&connect_timeout=1"
	if _, err := app.New(context.Background(), nodb, nil, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Fatal("db down")
	}
}
