package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tropa/internal/app"
	"tropa/internal/domain"
	"tropa/internal/store/storetest"
)

type harness struct {
	t   *testing.T
	app *app.App
	srv *httptest.Server
}

func testConfig() app.Config {
	start, _ := time.Parse(time.RFC3339, "2026-11-09T09:30:00+03:00")
	return app.Config{DemoMode: true, SimStart: start, CorpToken: "test-token", SessionSecret: "test-secret", DisableCron: true, CronInterval: time.Hour, LLMProvider: "none", LogLevel: "error"}
}

func newHarness(t *testing.T, mut ...func(*app.Config)) *harness {
	t.Helper()
	cfg := testConfig()
	for _, m := range mut {
		m(&cfg)
	}
	st := storetest.New(t)
	a, err := app.New(context.Background(), cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	return &harness{t: t, app: a, srv: srv}
}

type client struct {
	h    *harness
	http *http.Client
}

func (h *harness) login(login string) *client {
	h.t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &client{h: h, http: &http.Client{Jar: jar}}
	var out map[string]any
	if code := c.do("POST", "/api/auth/login", map[string]string{"login": login, "password": "buddy2026"}, &out, false); code != 200 {
		h.t.Fatalf("login %s: %d %v", login, code, out)
	}
	return c
}

func (c *client) do(method, path string, body, out any, csrf bool) int {
	c.h.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.h.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	if csrf {
		req.Header.Set("X-Requested-With", "tropa")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		raw, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(raw, out); err != nil {
			c.h.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode
}

func (c *client) get(path string, out any) int { return c.do("GET", path, nil, out, false) }

func (c *client) post(path string, body, out any) int {
	if body == nil {
		body = map[string]any{}
	}
	return c.do("POST", path, body, out, true)
}

type reply struct {
	User    domain.Message  `json:"user"`
	Buddy   domain.Message  `json:"buddy"`
	Actions []domain.Action `json:"actions"`
	Error   *apiErr         `json:"error"`
}

type apiErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *client) chat(text string) reply {
	c.h.t.Helper()
	var r reply
	if code := c.post("/api/chat", map[string]string{"text": text}, &r); code != 200 {
		c.h.t.Fatalf("chat %q: %d %+v", text, code, r.Error)
	}
	return r
}

func (r reply) action(t *testing.T) domain.Action {
	t.Helper()
	if len(r.Actions) == 0 {
		t.Fatalf("expected an action card, got text %q", r.Buddy.Text)
	}
	return r.Actions[0]
}

func (r reply) card(typ string) *domain.Card {
	for i := range r.Buddy.Payload.Cards {
		if r.Buddy.Payload.Cards[i].Type == typ {
			return &r.Buddy.Payload.Cards[i]
		}
	}
	return nil
}

func (c *client) confirm(id string) domain.Action {
	c.h.t.Helper()
	var a domain.Action
	if code := c.post("/api/actions/"+id+"/confirm", nil, &a); code != 200 {
		c.h.t.Fatalf("confirm %s: %d", id, code)
	}
	return a
}

type routeView struct {
	Day     int `json:"day"`
	Done    int `json:"done"`
	Total   int `json:"total"`
	Overdue int `json:"overdue"`
	Stages  []struct {
		ID    string `json:"id"`
		Tasks []struct {
			ID          string   `json:"id"`
			Status      string   `json:"status"`
			Owner       string   `json:"owner"`
			BlockedBy   []string `json:"blocked_by"`
			WaitingFor  string   `json:"waiting_for"`
			CanComplete bool     `json:"can_complete"`
			CanNudge    bool     `json:"can_nudge"`
			Overdue     bool     `json:"overdue"`
		} `json:"tasks"`
	} `json:"stages"`
	Next         []domain.StepItem `json:"next"`
	Waiting      []domain.StepItem `json:"waiting"`
	WhileWaiting []domain.StepItem `json:"while_waiting"`
}

func (c *client) route(user string) routeView {
	c.h.t.Helper()
	var v routeView
	p := "/api/route"
	if user != "" {
		p += "?user=" + user
	}
	if code := c.get(p, &v); code != 200 {
		c.h.t.Fatalf("route: %d", code)
	}
	return v
}

func (v routeView) status(id string) string {
	for _, s := range v.Stages {
		for _, t := range s.Tasks {
			if t.ID == id {
				return t.Status
			}
		}
	}
	return ""
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func mustContain(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Fatalf("expected %q in:\n%s", sub, s)
		}
	}
}
