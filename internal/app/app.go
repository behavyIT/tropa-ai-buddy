// Package app wires all components together.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"tropa/content"
	"tropa/internal/actions"
	"tropa/internal/agent"
	"tropa/internal/auth"
	"tropa/internal/catalog"
	"tropa/internal/clock"
	"tropa/internal/corp"
	"tropa/internal/corpmock"
	"tropa/internal/cron"
	"tropa/internal/directory"
	"tropa/internal/flags"
	"tropa/internal/httpapi"
	"tropa/internal/insights"
	"tropa/internal/kb"
	"tropa/internal/llm"
	"tropa/internal/mcp"
	"tropa/internal/mentor"
	"tropa/internal/notify"
	"tropa/internal/policy"
	"tropa/internal/ratelimit"
	"tropa/internal/route"
	"tropa/internal/seed"
	"tropa/internal/store"
	"tropa/internal/webui"
)

// App is the assembled application.
type App struct {
	Cfg      Config
	Log      *slog.Logger
	Store    *store.Store
	Clock    clock.Clock
	Sim      *clock.Sim
	Dir      *directory.Directory
	Catalog  *catalog.Catalog
	KB       *kb.Index
	MCP      *mcp.Client
	CorpMock *corpmock.Mock // non-nil in embedded mode
	Admin    corp.Admin
	Actions  *actions.Service
	Mentor   *mentor.Service
	Cron     *cron.Cron
	Handler  http.Handler
	redis    *redis.Client
}

// New builds the app; st may be injected (tests) or nil to open cfg.DatabaseURL.
func New(ctx context.Context, cfg Config, st *store.Store, log *slog.Logger) (*App, error) {
	a := &App{Cfg: cfg, Log: log}
	var err error
	if st == nil {
		if st, err = store.Open(ctx, cfg.DatabaseURL, cfg.DBSchema); err != nil {
			return nil, err
		}
		if err := st.Migrate(ctx); err != nil {
			return nil, err
		}
	}
	a.Store = st
	if a.Catalog, err = catalog.Load(content.FS); err != nil {
		return nil, err
	}
	a.Dir = directory.New()
	pol := policy.New(a.Dir)

	// clock: simulated in demo mode (time machine), wall clock otherwise
	if cfg.DemoMode {
		a.Sim = clock.NewSim(cfg.SimStart, nil)
		if base, setAt, ok, err := st.LoadClock(ctx); err == nil && ok {
			a.Sim.Set(base.Add(time.Since(setAt)))
		} else {
			_ = st.SaveClock(ctx, cfg.SimStart, time.Now())
		}
		a.Clock = a.Sim
	} else {
		a.Clock = clock.Real{}
	}

	// corporate services over MCP
	if cfg.CorpMCPURL == "" {
		a.CorpMock = corpmock.New(a.Dir, a.Catalog, cfg.CorpToken, a.Clock.Now)
		a.CorpMock.Latency = cfg.CorpLatency
		a.MCP = mcp.NewClient("http://corp.internal/mcp", cfg.CorpToken, 15*time.Second)
		a.MCP.HTTP.Transport = mcp.HandlerTransport{Handler: a.CorpMock.Handler(cfg.CorpToken)}
		a.Admin = mockAdmin{a.CorpMock}
	} else {
		a.MCP = mcp.NewClient(cfg.CorpMCPURL, cfg.CorpToken, 15*time.Second)
		a.Admin = corp.NewHTTPAdmin(cfg.CorpMCPURL, cfg.CorpToken)
	}
	if cfg.DemoMode {
		// the time machine must also apply to corporate services running as separate processes
		a.MCP.DemoNow = a.Clock.Now
	}
	corpSvc := &corp.Client{MCP: a.MCP}

	// rate limiting / cron leader lock
	var limiter ratelimit.Limiter
	var locker ratelimit.Locker
	if cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			return nil, err
		}
		a.redis = redis.NewClient(opt)
		r := &ratelimit.Redis{C: a.redis}
		limiter, locker = r, r
	} else {
		m := ratelimit.NewMemory()
		limiter, locker = m, m
	}

	var provider llm.Provider
	llmName := "offline"
	if cfg.LLMProvider == "anthropic" {
		p := llm.NewAnthropic(llm.AnthropicConfig{APIKey: cfg.AnthropicKey, BaseURL: cfg.AnthropicURL, Model: cfg.LLMModel, Effort: cfg.LLMEffort, ServerFallback: cfg.LLMFallback})
		provider, llmName = p, p.Name()
	}

	notifier := notify.New(st, a.Clock, log)
	fl := flags.New(st)
	a.KB = kb.New(a.Catalog, a.Dir, pol)
	rt := route.New(a.Catalog, a.Dir, pol, st)
	a.Mentor = mentor.New(st, a.Clock, a.Dir, pol, notifier, a.KB)
	a.Actions = actions.New(st, a.Clock, a.Dir, a.Catalog, pol, corpSvc, notifier, fl, a.Mentor, rt, log)
	ins := insights.New(st, a.Clock, a.Dir, a.Catalog, pol, rt, a.KB)
	ag := agent.New(agent.Deps{Store: st, Clock: a.Clock, Dir: a.Dir, Catalog: a.Catalog, Policy: pol, KB: a.KB, Route: rt, LLM: provider,
		Flags: fl, Actions: a.Actions, Notify: notifier, Log: log})
	a.Cron = cron.New(st, a.Clock, a.Dir, rt, a.Actions, notifier, locker, log)

	if err := seed.Run(ctx, st); err != nil {
		return nil, err
	}
	if err := a.Mentor.RebuildIndex(ctx); err != nil {
		return nil, err
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector())
	metrics := httpapi.NewMetrics(reg)
	api := httpapi.New(httpapi.Deps{Auth: auth.New(cfg.SessionSecret, a.Dir, 8*time.Hour, cfg.SecureCookie), Dir: a.Dir, Catalog: a.Catalog, Policy: pol,
		Store: st, Clock: a.Clock, Route: rt, Agent: ag, Actions: a.Actions, Mentor: a.Mentor, Insights: ins, Flags: fl, Notify: notifier,
		Limiter: limiter, Metrics: metrics, Demo: &demo{a: a}, LLMName: llmName, Log: log, Ready: a.ready})
	mux := http.NewServeMux()
	api.Register(mux)
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Неизвестный метод API"}}`))
	})
	mux.Handle("/", webui.Handler())
	a.Handler = httpapi.Middleware(mux, metrics, log)
	return a, nil
}

func (a *App) ready(ctx context.Context) map[string]string {
	out := map[string]string{"db": "ok", "corp_mcp": "ok", "redis": "ok"}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := a.Store.Ping(cctx); err != nil {
		out["db"] = err.Error()
	}
	if _, err := a.MCP.ListTools(cctx); err != nil {
		out["corp_mcp"] = err.Error()
	}
	if a.redis != nil {
		if err := a.redis.Ping(cctx).Err(); err != nil {
			out["redis"] = err.Error()
		}
	} else {
		out["redis"] = "ok (in-memory fallback)"
	}
	return out
}

type mockAdmin struct{ m *corpmock.Mock }

func (m mockAdmin) Reset(context.Context) error               { m.m.Reset(); return nil }
func (m mockAdmin) SetFault(_ context.Context, on bool) error { m.m.SetFault(on); return nil }

// demo implements httpapi.Demo.
type demo struct {
	a     *App
	fault bool
}

func (d *demo) State(context.Context) httpapi.DemoState {
	now := d.a.Clock.Now()
	return httpapi.DemoState{Enabled: d.a.Cfg.DemoMode, Now: now, Today: route.FormatDate(now), Day: clock.DayOfInternship(directory.InternshipStart, now), CorpFault: d.fault}
}

func (d *demo) Advance(ctx context.Context, days int) error {
	if d.a.Sim == nil {
		return errors.New("simulated clock is off")
	}
	t := d.a.Sim.AdvanceDays(days)
	if err := d.a.Store.SaveClock(ctx, t, time.Now()); err != nil {
		return err
	}
	return d.a.Cron.Tick(ctx)
}

func (d *demo) Reset(ctx context.Context) error {
	if err := d.a.Store.Truncate(ctx); err != nil {
		return err
	}
	if d.a.Sim != nil {
		d.a.Sim.Set(d.a.Cfg.SimStart)
		if err := d.a.Store.SaveClock(ctx, d.a.Cfg.SimStart, time.Now()); err != nil {
			return err
		}
	}
	if err := d.a.Admin.Reset(ctx); err != nil {
		d.a.Log.Warn("corp reset failed", "err", err)
	}
	d.fault = false
	d.a.MCP.Reset()
	if err := seed.Run(ctx, d.a.Store); err != nil {
		return err
	}
	if err := d.a.Mentor.RebuildIndex(ctx); err != nil {
		return err
	}
	return d.a.Cron.Tick(ctx) // the intern immediately gets the morning brief of day 1
}

func (d *demo) SetCorpFault(ctx context.Context, on bool) error {
	if err := d.a.Admin.SetFault(ctx, on); err != nil {
		return err
	}
	d.fault = on
	if !on {
		d.a.MCP.Reset()
	}
	return nil
}

// Run serves HTTP and runs cron until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	srv := &http.Server{Addr: a.Cfg.Addr, Handler: a.Handler, ReadHeaderTimeout: 10 * time.Second}
	if !a.Cfg.DisableCron {
		go a.Cron.Run(ctx, a.Cfg.CronInterval)
	}
	errc := make(chan error, 1)
	go func() {
		a.Log.Info("tropa listening", "addr", a.Cfg.Addr, "demo", a.Cfg.DemoMode, "corp", a.Cfg.CorpMCPURL == "")
		errc <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	case err := <-errc:
		return err
	}
}

// Close releases resources.
func (a *App) Close() {
	if a.redis != nil {
		_ = a.redis.Close()
	}
	a.Store.Close()
}

// NewLogger creates a JSON logger.
func NewLogger(level string) *slog.Logger {
	var l slog.Level
	_ = l.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}
