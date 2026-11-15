package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"tropa/internal/app"
	"tropa/internal/domain"
)

// ---------- SEC: безопасность и права ----------

func TestSEC01_SensitiveTopicsRoutedToHRBP(t *testing.T) {
	h := newHarness(t)
	for _, q := range []string{"Сколько получает Артём?", "Как оформить больничный?", "Скажи паспортные данные Марии"} {
		r := h.login("alex").chat(q)
		if r.Buddy.Payload.Intent != "sensitive" || r.card("person") == nil {
			t.Fatalf("%q => %s", q, r.Buddy.Payload.Intent)
		}
	}
}

func TestSEC02_AuthRequired(t *testing.T) {
	h := newHarness(t)
	resp, err := http.Get(h.srv.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("me without session: %d", resp.StatusCode)
	}
	c := &client{h: h, http: &http.Client{}}
	if code := c.do("POST", "/api/auth/login", map[string]string{"login": "alex", "password": "wrong"}, &map[string]any{}, false); code != 401 {
		t.Fatalf("wrong password: %d", code)
	}
	if code := c.do("POST", "/api/auth/login", map[string]string{"login": "ghost", "password": "buddy2026"}, &map[string]any{}, false); code != 401 {
		t.Fatalf("unknown user: %d", code)
	}
	alex := h.login("alex")
	var me struct {
		User struct {
			Role string `json:"role"`
		} `json:"user"`
		Mentor struct {
			Name string `json:"name"`
		} `json:"mentor"`
	}
	alex.get("/api/me", &me)
	if me.User.Role != "intern" || me.Mentor.Name != "Артём Ким" {
		t.Fatalf("me: %+v", me)
	}
	alex.post("/api/auth/logout", nil, &map[string]any{})
	if code := alex.get("/api/me", &map[string]any{}); code != 401 {
		t.Fatalf("after logout: %d", code)
	}
}

func TestSEC03_CSRFHeaderRequired(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	if code := alex.do("POST", "/api/chat", map[string]string{"text": "привет"}, &map[string]any{}, false); code != 403 {
		t.Fatalf("no csrf header: %d", code)
	}
}

func TestSEC04_RateLimits(t *testing.T) {
	h := newHarness(t)
	c := &client{h: h, http: &http.Client{}}
	last := 0
	for i := 0; i < 11; i++ {
		last = c.do("POST", "/api/auth/login", map[string]string{"login": "alex", "password": "x"}, &map[string]any{}, false)
	}
	if last != 429 {
		t.Fatalf("login brute force must be limited: %d", last)
	}
}

func TestSEC05_ApiPermissionChecks(t *testing.T) {
	h := newHarness(t)
	alex, dana, artem := h.login("alex"), h.login("dana"), h.login("artem")
	if code := dana.get("/api/route?user=u-intern-alex", &map[string]any{}); code != 404 {
		t.Fatalf("intern sees another intern: %d", code)
	}
	if code := h.login("ilya").get("/api/route?user=u-intern-alex", &map[string]any{}); code != 404 {
		t.Fatalf("foreign mentor: %d", code)
	}
	if code := artem.get("/api/route?user=u-intern-alex", &map[string]any{}); code != 200 {
		t.Fatalf("own mentor: %d", code)
	}
	if code := h.login("olga").get("/api/route?user=u-intern-dana", &map[string]any{}); code != 200 {
		t.Fatalf("hrbp: %d", code)
	}
	if code := artem.get("/api/route", &map[string]any{}); code != 404 {
		t.Fatalf("mentor has no route: %d", code)
	}
	if code := alex.get("/api/approvals", &map[string]any{}); code != 403 {
		t.Fatalf("intern approvals: %d", code)
	}
	if code := alex.get("/api/insights", &map[string]any{}); code != 403 {
		t.Fatalf("intern insights: %d", code)
	}
	if code := alex.do("PUT", "/api/flags/teachback", map[string]any{"enabled": false}, &map[string]any{}, true); code != 403 {
		t.Fatalf("intern flags: %d", code)
	}
	if code := artem.post("/api/route/tasks/add-to-chats/nudge", nil, &map[string]any{}); code != 403 {
		t.Fatalf("mentor nudge: %d", code)
	}
	if code := artem.post("/api/route/tasks/notes/start", nil, &map[string]any{}); code != 403 {
		t.Fatalf("mentor start: %d", code)
	}
	if code := h.login("maria").post("/api/approvals/nope/decide", map[string]any{"approve": true}, &map[string]any{}); code != 404 {
		t.Fatalf("unknown approval: %d", code)
	}
}

func TestSEC06_ChatIsPrivate(t *testing.T) {
	h := newHarness(t)
	h.login("alex").chat("Мне кажется, я не справляюсь, это нормально?")
	var hist struct {
		Messages []domain.Message `json:"messages"`
	}
	h.login("artem").get("/api/chat", &hist)
	if len(hist.Messages) != 0 {
		t.Fatal("mentor must not see the intern's chat")
	}
}

// ---------- CRON: проактивность ----------

func TestCRON01_MorningBriefOncePerDay(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	var st struct {
		Day int `json:"day"`
	}
	if code := alex.post("/api/demo/advance", map[string]int{"days": 1}, &st); code != 200 || st.Day != 2 {
		t.Fatalf("advance: %d %+v", code, st)
	}
	if err := h.app.Cron.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	var hist struct {
		Messages []domain.Message `json:"messages"`
	}
	alex.get("/api/chat", &hist)
	briefs := 0
	for _, m := range hist.Messages {
		if m.Payload.Intent == "morning_brief" {
			briefs++
			mustContain(t, m.Text, "День 2")
		}
	}
	if briefs != 1 {
		t.Fatalf("briefs: %d", briefs)
	}
}

func TestCRON02_OverdueRemindersAndMentorNudges(t *testing.T) {
	h := newHarness(t)
	alex, artem, maria := h.login("alex"), h.login("artem"), h.login("maria")
	alex.post("/api/demo/advance", map[string]int{"days": 4}, &map[string]any{})
	var n struct {
		Notifications []domain.Notification `json:"notifications"`
	}
	artem.get("/api/notifications", &n)
	if !anyKind(n.Notifications, "nudge") {
		t.Fatalf("mentor must be nudged: %+v", n)
	}
	maria.get("/api/notifications", &n)
	if !anyKind(n.Notifications, "nudge") {
		t.Fatalf("lead must be nudged: %+v", n)
	}
	alex.get("/api/notifications", &n)
	if !anyKind(n.Notifications, "reminder") {
		t.Fatalf("intern reminder: %+v", n)
	}
	if v := alex.route(""); v.Overdue == 0 {
		t.Fatal("overdue counter")
	}
}

func TestCRON03_StatusSyncFromService(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	a := alex.chat("Нужен доступ к kafka").action(t)
	done := alex.confirm(a.ID)
	// decision made directly in the corporate service (not through Тропа)
	if _, err := h.app.CorpMock.Server.Tools(), error(nil); err != nil {
		t.Fatal(err)
	}
	var res struct {
		Requests []struct {
			RequestID string `json:"request_id"`
		} `json:"requests"`
	}
	_ = json.Unmarshal(done.Result, &res)
	decideInCorp(t, h, res.Requests[0].RequestID)
	if err := h.app.Cron.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	var got domain.Action
	alex.get("/api/actions/"+a.ID, &got)
	if got.ExternalStatus != domain.ExtGranted {
		t.Fatalf("sync: %+v", got)
	}
}

func TestCRON04_MentorDigest(t *testing.T) {
	h := newHarness(t)
	alex, artem := h.login("alex"), h.login("artem")
	alex.confirm(alex.chat("Спроси наставника: где лежат шаблоны сервисов?").action(t).ID)
	alex.post("/api/demo/advance", map[string]int{"days": 1}, &map[string]any{})
	var n struct {
		Notifications []domain.Notification `json:"notifications"`
	}
	artem.get("/api/notifications", &n)
	if !anyKind(n.Notifications, "digest") {
		t.Fatalf("digest: %+v", n)
	}
	var ok map[string]bool
	artem.post("/api/notifications/read", nil, &ok)
	var after struct {
		Unread int `json:"unread"`
	}
	artem.get("/api/notifications", &after)
	if after.Unread != 0 {
		t.Fatal("read all")
	}
}

func anyKind(ns []domain.Notification, kind string) bool {
	for _, n := range ns {
		if n.Kind == kind {
			return true
		}
	}
	return false
}

func decideInCorp(t *testing.T, h *harness, requestID string) {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"access_decide","arguments":{"request_id":"` + requestID + `","decision":"approve"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-On-Behalf-Of", "u-lead-maria")
	rec := httptest.NewRecorder()
	h.app.CorpMock.Handler("test-token").ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "granted") {
		t.Fatalf("corp decide: %s", rec.Body.String())
	}
}

// ---------- INS: «Тропинки» ----------

func TestINS01_ScopeByRole(t *testing.T) {
	h := newHarness(t)
	var lead, hr struct {
		Scope   string `json:"scope"`
		Interns []struct {
			Name string `json:"name"`
		} `json:"interns"`
	}
	h.login("maria").get("/api/insights", &lead)
	h.login("olga").get("/api/insights", &hr)
	if len(lead.Interns) != 1 || len(hr.Interns) != 2 || !contains(lead.Scope, "Объявления") {
		t.Fatalf("lead=%+v hr=%+v", lead, hr)
	}
	var team struct {
		Interns []map[string]any `json:"interns"`
	}
	h.login("artem").get("/api/team", &team)
	if len(team.Interns) != 1 {
		t.Fatalf("mentor team: %+v", team)
	}
}

func TestINS02_MetricsHotspotsAndPrivateGaps(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	alex.chat("Как проходит код-ревью?")
	alex.chat("Как настроить принтер в переговорке на пятом этаже?")
	alex.confirm(alex.chat("Запроси стандартные доступы").action(t).ID)
	var rep struct {
		Metrics struct {
			Answered           int `json:"answered"`
			Unanswered         int `json:"unanswered"`
			ActionsExecuted    int `json:"actions_executed"`
			MentorMinutesSaved int `json:"mentor_minutes_saved"`
		} `json:"metrics"`
		Hotspots []struct {
			TaskID string `json:"task_id"`
		} `json:"hotspots"`
		Gaps []struct {
			Topic  string `json:"topic"`
			Source string `json:"source"`
			Status string `json:"status"`
		} `json:"gaps"`
		Synthetic int `json:"synthetic_events"`
	}
	h.login("maria").get("/api/insights", &rep)
	m := rep.Metrics
	if m.Answered < 1 || m.Unanswered < 1 || m.ActionsExecuted != 1 || m.MentorMinutesSaved == 0 || rep.Synthetic == 0 || len(rep.Hotspots) == 0 {
		t.Fatalf("report: %+v", rep)
	}
	for _, g := range rep.Gaps {
		if strings.Contains(g.Topic, "переговорке на пятом") {
			t.Fatal("raw chat text must not appear in analytics")
		}
	}
	// once the same question is escalated to the mentor, it is listed once (with its status)
	alex.confirm(alex.chat("Как настроить принтер в переговорке на пятом этаже?").action(t).ID)
	h.login("maria").get("/api/insights", &rep)
	n := 0
	for _, g := range rep.Gaps {
		if strings.Contains(g.Topic, "принтер") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("gap should be deduplicated: %+v", rep.Gaps)
	}
	closed := false
	for _, g := range rep.Gaps {
		closed = closed || (g.Topic == "staging токен" && g.Status == "закрыт ответом в базе")
	}
	if !closed {
		t.Fatalf("synthetic gap answered by a seeded mentor card must be marked closed: %+v", rep.Gaps)
	}
}

// ---------- FLAG: фича-флаги ----------

func TestFLAG01_TeamOverridesAndPermissions(t *testing.T) {
	h := newHarness(t)
	maria, olga := h.login("maria"), h.login("olga")
	if code := maria.do("PUT", "/api/flags/teachback", map[string]any{"enabled": false, "scope": "global"}, &map[string]any{}, true); code != 403 {
		t.Fatalf("lead global flag: %d", code)
	}
	if code := maria.do("PUT", "/api/flags/nope", map[string]any{"enabled": false}, &map[string]any{}, true); code != 404 {
		t.Fatalf("unknown flag: %d", code)
	}
	if code := olga.do("PUT", "/api/flags/teachback", map[string]any{"enabled": false, "scope": "global"}, &map[string]any{}, true); code != 200 {
		t.Fatalf("hrbp global: %d", code)
	}
	mustContain(t, h.login("alex").chat("хочу рассказать про sentry").Buddy.Text, "выключен")
	maria.do("PUT", "/api/flags/teachback", map[string]any{"enabled": true}, &map[string]any{}, true)
	var fl struct {
		Flags []struct {
			Key     string `json:"key"`
			Enabled bool   `json:"enabled"`
			Source  string `json:"source"`
		} `json:"flags"`
	}
	maria.get("/api/flags", &fl)
	for _, f := range fl.Flags {
		if f.Key == "teachback" && (!f.Enabled || f.Source != "team") {
			t.Fatalf("team override: %+v", f)
		}
	}
	mustContain(t, h.login("alex").chat("хочу рассказать про sentry").Buddy.Text, "Давай")
	mustContain(t, h.login("dana").chat("хочу рассказать про sentry").Buddy.Text, "выключен")
	// access & mentor flags
	maria.do("PUT", "/api/flags/actions.access", map[string]any{"enabled": false}, &map[string]any{}, true)
	mustContain(t, h.login("alex").chat("запроси доступ к jira").Buddy.Text, "выключены")
	maria.do("PUT", "/api/flags/mentor.digest", map[string]any{"enabled": false}, &map[string]any{}, true)
	mustContain(t, h.login("alex").chat("спроси наставника: как дела у команды на проде?").Buddy.Text, "выключена")
}

// ---------- DEMO ----------

func TestDEMO01_ResetAndHealth(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	alex.confirm(alex.chat("Запроси стандартные доступы").action(t).ID)
	alex.post("/api/demo/advance", map[string]int{"days": 2}, &map[string]any{})
	var st struct {
		Day int `json:"day"`
	}
	if code := alex.post("/api/demo/reset", nil, &st); code != 200 || st.Day != 1 {
		t.Fatalf("reset: %d %+v", code, st)
	}
	if alex.route("").Done != 0 {
		t.Fatal("route must be reset")
	}
	if code := alex.post("/api/demo/advance", map[string]int{"days": 99}, &map[string]any{}); code != 400 {
		t.Fatalf("bad days: %d", code)
	}
	for _, p := range []string{"/healthz", "/readyz", "/metrics", "/", "/some/spa/route"} {
		resp, err := http.Get(h.srv.URL + p)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: %v %v", p, err, resp)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if p == "/metrics" && !strings.Contains(string(b), "tropa_http_requests_total") {
			t.Fatal("metrics")
		}
	}
	if code := alex.get("/api/nope", &map[string]any{}); code != 404 {
		t.Fatalf("unknown api: %d", code)
	}
	var ds map[string]any
	alex.get("/api/demo", &ds)
	if ds["enabled"] != true {
		t.Fatal("demo state")
	}
}

func TestDEMO02_DemoModeOff(t *testing.T) {
	h := newHarness(t, func(c *app.Config) { c.DemoMode = false })
	alex := h.login("alex")
	if code := alex.post("/api/demo/advance", map[string]int{"days": 1}, &map[string]any{}); code != 403 {
		t.Fatalf("advance in prod mode: %d", code)
	}
	if code := alex.post("/api/demo/reset", nil, &map[string]any{}); code != 403 {
		t.Fatalf("reset in prod mode: %d", code)
	}
	if code := alex.post("/api/demo/corp-fault", map[string]bool{"enabled": true}, &map[string]any{}); code != 403 {
		t.Fatalf("fault in prod mode: %d", code)
	}
}

// ---------- LLM: языковой слой поверх детерминированного оркестратора ----------

// fakeAnthropic emulates the Messages API so the real SDK path is exercised.
func fakeAnthropic(t *testing.T, mode *atomic.Value) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		m, _ := mode.Load().(string)
		if m == "down" {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"boom"}}`))
			return
		}
		text := `Отгул согласует руководитель [1].`
		stop := "end_turn"
		switch {
		case m == "refuse":
			text, stop = "", "refusal"
		case strings.Contains(body, `"intent"`) && strings.Contains(body, "json_schema") && strings.Contains(body, "среду"):
			text = `{"intent":"absence","sub":"do","date_from":"2026-11-18","date_to":"2026-11-18","systems":[],"topic":"","standard":false,"confidence":0.9}`
		case strings.Contains(body, `"intent"`) && strings.Contains(body, "json_schema"):
			text = `{"intent":"question","sub":"","date_from":"","date_to":"","systems":[],"topic":"","standard":false,"confidence":0.8}`
		case strings.Contains(body, `"covered"`):
			text = `{"covered":["errors","context","grouping","alerts"],"feedback":"Здорово!"}`
		case m == "noanswer":
			text = "НЕТ_ОТВЕТА"
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
			"content": []map[string]any{{"type": "text", "text": text}}, "stop_reason": stop, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 10}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func llmHarness(t *testing.T, mode *atomic.Value) *harness {
	srv := fakeAnthropic(t, mode)
	t.Cleanup(srv.Close)
	return newHarness(t, func(c *app.Config) {
		c.LLMProvider, c.AnthropicKey, c.AnthropicURL, c.LLMModel, c.LLMFallback = "anthropic", "test", srv.URL, "claude-opus-5-5", true
	})
}

func TestLLM01_UnderstandAndComposeWithStructuredOutputs(t *testing.T) {
	mode := &atomic.Value{}
	mode.Store("ok")
	h := llmHarness(t, mode)
	alex := h.login("alex")
	r := alex.chat("слушай, мне бы в среду через неделю не приходить")
	if r.Buddy.Payload.Mode != "llm" || len(r.Actions) != 1 || !contains(r.Actions[0].Title, "18 ноября") {
		t.Fatalf("llm understand: mode=%s text=%q", r.Buddy.Payload.Mode, r.Buddy.Text)
	}
	q := alex.chat("Как проходит код-ревью?")
	if q.Buddy.Payload.Mode != "llm" || !contains(q.Buddy.Text, "[1]") {
		t.Fatalf("compose: %+v", q.Buddy)
	}
	alex.chat("хочу рассказать про sentry")
	g := alex.chat("Ну там ловятся ошибки, видно что случилось и где, похожие склеиваются, и приходят уведомления.")
	if tb := g.card("teachback"); tb == nil || !tb.Teachback.Passed {
		t.Fatalf("llm grade: %+v", g.Buddy.Payload)
	}
}

func TestLLM02_FailuresDegradeToOffline(t *testing.T) {
	mode := &atomic.Value{}
	mode.Store("down")
	h := llmHarness(t, mode)
	alex := h.login("alex")
	r := alex.chat("Как проходит код-ревью?")
	if r.Buddy.Payload.Mode != "llm_fallback" || !contains(r.Buddy.Text, "[1]") {
		t.Fatalf("fallback: %+v", r.Buddy.Payload)
	}
	u := alex.chat("ну это, хотелось бы в среду через неделю не приходить")
	if u.Buddy.Payload.Mode != "llm_fallback" {
		t.Fatalf("understand fallback: %s", u.Buddy.Payload.Mode)
	}
	alex.chat("хочу рассказать про grafana")
	gr := alex.chat("Grafana показывает дашборды с панелями, данные берёт из Prometheus запросами PromQL, умеет алерты.")
	if gr.Buddy.Payload.Mode != "llm_fallback" || gr.card("teachback") == nil {
		t.Fatalf("grade fallback: %+v", gr.Buddy.Payload)
	}
	mode.Store("refuse")
	if rr := alex.chat("Как проходит код-ревью у нас?"); rr.Buddy.Payload.Mode != "llm_fallback" {
		t.Fatalf("refusal fallback: %s", rr.Buddy.Payload.Mode)
	}
	mode.Store("noanswer")
	na := alex.chat("Как проходит код-ревью в команде?")
	if na.Buddy.Payload.Mode != "llm" || len(na.Buddy.Text) == 0 {
		t.Fatalf("no-answer → offline compose: %+v", na.Buddy.Payload)
	}
	var me struct {
		LLM string `json:"llm"`
	}
	alex.get("/api/me", &me)
	if me.LLM != "anthropic:claude-opus-5-5" {
		t.Fatalf("llm name: %s", me.LLM)
	}
}
