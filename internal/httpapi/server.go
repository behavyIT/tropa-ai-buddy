package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"tropa/internal/actions"
	"tropa/internal/agent"
	"tropa/internal/apperr"
	"tropa/internal/auth"
	"tropa/internal/catalog"
	"tropa/internal/clock"
	"tropa/internal/directory"
	"tropa/internal/domain"
	"tropa/internal/flags"
	"tropa/internal/insights"
	"tropa/internal/mentor"
	"tropa/internal/notify"
	"tropa/internal/policy"
	"tropa/internal/ratelimit"
	"tropa/internal/route"
	"tropa/internal/store"
)

// DemoState describes the simulated environment.
type DemoState struct {
	Enabled   bool      `json:"enabled"`
	Now       time.Time `json:"now"`
	Today     string    `json:"today"`
	Day       int       `json:"day"`
	CorpFault bool      `json:"corp_fault"`
}

// Demo is the demo control panel (time machine, reset, outage).
type Demo interface {
	State(ctx context.Context) DemoState
	Advance(ctx context.Context, days int) error
	Reset(ctx context.Context) error
	SetCorpFault(ctx context.Context, on bool) error
}

// Server holds API dependencies.
type Server struct {
	auth     *auth.Auth
	dir      *directory.Directory
	cat      *catalog.Catalog
	pol      *policy.Policy
	st       *store.Store
	clk      clock.Clock
	route    *route.Engine
	agent    *agent.Agent
	actions  *actions.Service
	mentor   *mentor.Service
	insights *insights.Service
	flags    *flags.Service
	notif    *notify.Notifier
	limiter  ratelimit.Limiter
	metrics  *Metrics
	demo     Demo
	llmName  string
	log      *slog.Logger
	ready    func(ctx context.Context) map[string]string
}

// Deps groups server dependencies.
type Deps struct {
	Auth     *auth.Auth
	Dir      *directory.Directory
	Catalog  *catalog.Catalog
	Policy   *policy.Policy
	Store    *store.Store
	Clock    clock.Clock
	Route    *route.Engine
	Agent    *agent.Agent
	Actions  *actions.Service
	Mentor   *mentor.Service
	Insights *insights.Service
	Flags    *flags.Service
	Notify   *notify.Notifier
	Limiter  ratelimit.Limiter
	Metrics  *Metrics
	Demo     Demo
	LLMName  string
	Log      *slog.Logger
	Ready    func(ctx context.Context) map[string]string
}

// New creates the API server.
func New(d Deps) *Server {
	return &Server{auth: d.Auth, dir: d.Dir, cat: d.Catalog, pol: d.Policy, st: d.Store, clk: d.Clock, route: d.Route, agent: d.Agent, actions: d.Actions,
		mentor: d.Mentor, insights: d.Insights, flags: d.Flags, notif: d.Notify, limiter: d.Limiter, metrics: d.Metrics, demo: d.Demo, llmName: d.LLMName, log: d.Log, ready: d.Ready}
}

// Register mounts API routes on mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/me", s.guard(s.me))
	mux.HandleFunc("GET /api/route", s.guard(s.getRoute))
	mux.HandleFunc("POST /api/route/tasks/{task}/start", s.guard(s.startTask))
	mux.HandleFunc("POST /api/route/tasks/{task}/complete", s.guard(s.completeTask))
	mux.HandleFunc("POST /api/route/tasks/{task}/nudge", s.guard(s.nudgeTask))
	mux.HandleFunc("GET /api/chat", s.guard(s.chatHistory))
	mux.HandleFunc("POST /api/chat", s.guard(s.chat))
	mux.HandleFunc("POST /api/chat/messages/{id}/feedback", s.guard(s.feedback))
	mux.HandleFunc("GET /api/actions", s.guard(s.listActions))
	mux.HandleFunc("GET /api/actions/{id}", s.guard(s.getAction))
	mux.HandleFunc("POST /api/actions/{id}/confirm", s.guard(s.confirmAction))
	mux.HandleFunc("POST /api/actions/{id}/cancel", s.guard(s.cancelAction))
	mux.HandleFunc("PATCH /api/actions/{id}", s.guard(s.editAction))
	mux.HandleFunc("GET /api/notifications", s.guard(s.notifications))
	mux.HandleFunc("POST /api/notifications/read", s.guard(s.readNotifications))
	mux.HandleFunc("GET /api/mentor/questions", s.guard(s.mentorQuestions))
	mux.HandleFunc("POST /api/mentor/questions/{id}/answer", s.guard(s.answerQuestion))
	mux.HandleFunc("GET /api/team", s.guard(s.team))
	mux.HandleFunc("GET /api/approvals", s.guard(s.approvals))
	mux.HandleFunc("POST /api/approvals/{id}/decide", s.guard(s.decide))
	mux.HandleFunc("GET /api/insights", s.guard(s.getInsights))
	mux.HandleFunc("GET /api/flags", s.guard(s.getFlags))
	mux.HandleFunc("PUT /api/flags/{key}", s.guard(s.setFlag))
	mux.HandleFunc("GET /api/demo", s.guard(s.demoState))
	mux.HandleFunc("POST /api/demo/advance", s.guard(s.demoAdvance))
	mux.HandleFunc("POST /api/demo/reset", s.guard(s.demoReset))
	mux.HandleFunc("POST /api/demo/corp-fault", s.guard(s.demoFault))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", s.readyz)
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	checks := s.ready(r.Context())
	status := http.StatusOK
	for _, v := range checks {
		if !strings.HasPrefix(v, "ok") {
			status = http.StatusServiceUnavailable
		}
	}
	writeJSON(w, status, checks)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if err := s.limit(r.Context(), "login:"+clientIP(r), 10, time.Minute); err != nil {
		writeErr(w, err)
		return
	}
	var b struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := decode(r, &b); err != nil {
		writeErr(w, err)
		return
	}
	p, tok, err := s.auth.Login(b.Login, b.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "unauthorized", "message": err.Error()}})
		return
	}
	s.auth.SetCookie(w, tok)
	s.notif.Track(r.Context(), p, "login", nil)
	writeJSON(w, http.StatusOK, map[string]any{"user": s.personView(p)})
}

func (s *Server) logout(w http.ResponseWriter, _ *http.Request) {
	s.auth.ClearCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// PersonView is a public view of a person.
type PersonView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Title    string `json:"title"`
	TeamID   string `json:"team_id"`
	TeamName string `json:"team_name"`
	Contact  string `json:"contact"`
}

func (s *Server) personView(p *directory.Person) PersonView {
	v := PersonView{ID: p.ID, Name: p.Name, Role: p.Role, Title: p.Title, TeamID: p.TeamID, Contact: p.Contact}
	if t, ok := s.cat.Teams[p.TeamID]; ok {
		v.TeamName = t.Name
	}
	return v
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	fl, err := s.flags.List(r.Context(), u.TeamID)
	if err != nil {
		return err
	}
	out := map[string]any{"user": s.personView(u), "flags": fl, "demo": s.demo.State(r.Context()), "llm": s.llmName}
	if m, ok := s.dir.ByID(u.MentorID); ok {
		out["mentor"] = s.personView(m)
	}
	if l, ok := s.dir.ByID(u.LeadID); ok {
		out["lead"] = s.personView(l)
	}
	writeJSON(w, 200, out)
	return nil
}

func (s *Server) targetIntern(r *http.Request, u *directory.Person) (*directory.Person, error) {
	id := r.URL.Query().Get("user")
	if id == "" {
		id = u.ID
	}
	if !s.pol.CanViewUser(u, id) {
		return nil, apperr.NotFound("стажёр не найден")
	}
	p, ok := s.dir.ByID(id)
	if !ok || p.Role != directory.RoleIntern {
		return nil, apperr.NotFound("маршрут есть только у стажёров")
	}
	return p, nil
}

func (s *Server) getRoute(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	in, err := s.targetIntern(r, u)
	if err != nil {
		return err
	}
	v, err := s.route.Build(r.Context(), u, in, s.clk.Now())
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}

func (s *Server) startTask(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	rt, err := s.route.RouteFor(u)
	if u.Role != directory.RoleIntern || err != nil {
		return apperr.Forbidden("маршрут есть только у стажёров")
	}
	t, ok := rt.Task(r.PathValue("task"))
	if !ok || t.Owner != "intern" {
		return apperr.NotFound("пункт не найден")
	}
	if err := s.st.SetTaskStatus(r.Context(), u.ID, t.ID, domain.TaskInProgress, s.clk.Now()); err != nil {
		return err
	}
	return s.getRoute(w, r, u)
}

func (s *Server) completeTask(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	in, err := s.targetIntern(r, u)
	if err != nil {
		return err
	}
	a, err := s.actions.PlanChecklist(r.Context(), u, in, r.PathValue("task"), "Отмечено в маршруте")
	if err != nil {
		return err
	}
	a, err = s.actions.Confirm(r.Context(), u, a.ID)
	if err != nil {
		return err
	}
	writeJSON(w, 200, a)
	return nil
}

func (s *Server) nudgeTask(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	rt, err := s.route.RouteFor(u)
	if u.Role != directory.RoleIntern || err != nil {
		return apperr.Forbidden("напоминать могут стажёры")
	}
	t, ok := rt.Task(r.PathValue("task"))
	if !ok || t.Owner == "intern" {
		return apperr.NotFound("пункт не найден")
	}
	if err := s.limit(r.Context(), "nudge:"+u.ID+":"+t.ID, 1, 12*time.Hour); err != nil {
		return apperr.TooMany("Уже напомнили — повторно можно через 12 часов.")
	}
	owner := u.MentorID
	if t.Owner == "lead" {
		owner = u.LeadID
	}
	s.notif.Notify(r.Context(), owner, "nudge", u.Name+" ждёт: «"+t.Title+"»", "Напоминание отправлено через Тропу. Отметьте пункт в маршруте стажёра, когда сделаете.", "intern:"+u.ID)
	s.notif.Track(r.Context(), u, "nudge_sent", map[string]any{"task": t.ID, "auto": false})
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (s *Server) chatHistory(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	msgs, err := s.agent.History(r.Context(), u)
	if err != nil {
		return err
	}
	if msgs == nil {
		msgs = []domain.Message{}
	}
	writeJSON(w, 200, map[string]any{"messages": msgs})
	return nil
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	if err := s.limit(r.Context(), "chat:"+u.ID, 30, time.Minute); err != nil {
		return err
	}
	var b struct {
		Text string `json:"text"`
	}
	if err := decode(r, &b); err != nil {
		return err
	}
	rep, err := s.agent.Handle(r.Context(), u, b.Text)
	if err != nil {
		return err
	}
	s.metrics.Turns.WithLabelValues(rep.Buddy.Payload.Intent, rep.Buddy.Payload.Mode).Inc()
	if rep.Actions == nil {
		rep.Actions = []domain.Action{}
	}
	writeJSON(w, 200, rep)
	return nil
}

func (s *Server) feedback(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	var b struct {
		Helpful bool `json:"helpful"`
	}
	if err := decode(r, &b); err != nil {
		return err
	}
	v := -1
	if b.Helpful {
		v = 1
	}
	m, err := s.st.SetFeedback(r.Context(), r.PathValue("id"), u.ID, v)
	if err == store.ErrNotFound {
		return apperr.NotFound("сообщение не найдено")
	}
	if err != nil {
		return err
	}
	s.notif.Track(r.Context(), u, "answer_feedback", map[string]any{"helpful": b.Helpful, "intent": m.Payload.Intent})
	writeJSON(w, 200, m)
	return nil
}

func (s *Server) listActions(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	as, err := s.actions.List(r.Context(), u)
	if err != nil {
		return err
	}
	if as == nil {
		as = []domain.Action{}
	}
	writeJSON(w, 200, map[string]any{"actions": as})
	return nil
}

func (s *Server) getAction(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	a, err := s.actions.Get(r.Context(), u, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, a)
	return nil
}

func (s *Server) confirmAction(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	a, err := s.actions.Confirm(r.Context(), u, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, a)
	return nil
}

func (s *Server) cancelAction(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	a, err := s.actions.Cancel(r.Context(), u, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, a)
	return nil
}

func (s *Server) editAction(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	var b struct {
		DateFrom string `json:"date_from"`
		DateTo   string `json:"date_to"`
		Comment  string `json:"comment"`
	}
	if err := decode(r, &b); err != nil {
		return err
	}
	from, err1 := time.ParseInLocation("2006-01-02", b.DateFrom, clock.MSK)
	to, err2 := time.ParseInLocation("2006-01-02", b.DateTo, clock.MSK)
	if err1 != nil || err2 != nil {
		return apperr.BadRequest("Даты в формате ГГГГ-ММ-ДД.")
	}
	a, err := s.actions.EditAbsence(r.Context(), u, r.PathValue("id"), from, to, b.Comment)
	if err != nil {
		return err
	}
	writeJSON(w, 200, a)
	return nil
}

func (s *Server) notifications(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	ns, unread, err := s.st.ListNotifications(r.Context(), u.ID, 30)
	if err != nil {
		return err
	}
	if ns == nil {
		ns = []domain.Notification{}
	}
	writeJSON(w, 200, map[string]any{"notifications": ns, "unread": unread})
	return nil
}

func (s *Server) readNotifications(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	if err := s.st.MarkAllRead(r.Context(), u.ID, s.clk.Now()); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (s *Server) mentorQuestions(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	qs, err := s.mentor.Inbox(r.Context(), u)
	if err != nil {
		return err
	}
	if qs == nil {
		qs = []mentor.View{}
	}
	writeJSON(w, 200, map[string]any{"questions": qs})
	return nil
}

func (s *Server) answerQuestion(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	var b struct {
		Answer  string `json:"answer"`
		Publish bool   `json:"publish"`
	}
	if err := decode(r, &b); err != nil {
		return err
	}
	if len([]rune(b.Answer)) > 4000 {
		return apperr.BadRequest("Ответ слишком длинный.")
	}
	q, err := s.mentor.Answer(r.Context(), u, r.PathValue("id"), b.Answer, b.Publish)
	if err != nil {
		return err
	}
	writeJSON(w, 200, q)
	return nil
}

func (s *Server) team(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	t, err := s.insights.Team(r.Context(), u)
	if err != nil {
		return err
	}
	if t == nil {
		t = []insights.InternProgress{}
	}
	writeJSON(w, 200, map[string]any{"interns": t})
	return nil
}

func (s *Server) approvals(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	as, err := s.actions.PendingApprovals(r.Context(), u)
	if err != nil {
		return err
	}
	if as == nil {
		as = []actions.Approval{}
	}
	writeJSON(w, 200, map[string]any{"approvals": as})
	return nil
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	var b struct {
		Approve bool   `json:"approve"`
		Comment string `json:"comment"`
	}
	if err := decode(r, &b); err != nil {
		return err
	}
	a, err := s.actions.Decide(r.Context(), u, r.PathValue("id"), b.Approve, b.Comment)
	if err != nil {
		return err
	}
	writeJSON(w, 200, a)
	return nil
}

func (s *Server) getInsights(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	rep, err := s.insights.Build(r.Context(), u)
	if err != nil {
		return err
	}
	writeJSON(w, 200, rep)
	return nil
}

func (s *Server) getFlags(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	team := r.URL.Query().Get("team")
	if team == "" {
		team = u.TeamID
	}
	fl, err := s.flags.List(r.Context(), team)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"flags": fl, "team": team})
	return nil
}

func (s *Server) setFlag(w http.ResponseWriter, r *http.Request, u *directory.Person) error {
	key := r.PathValue("key")
	if !flags.Known(key) {
		return apperr.NotFound("неизвестный флаг")
	}
	var b struct {
		Enabled bool   `json:"enabled"`
		Scope   string `json:"scope"`
	}
	if err := decode(r, &b); err != nil {
		return err
	}
	if b.Scope == "" {
		b.Scope = "team:" + u.TeamID
	}
	if !s.pol.CanManageFlag(u, b.Scope) {
		return apperr.Forbidden("Нет прав менять этот флаг.")
	}
	if err := s.flags.Set(r.Context(), key, b.Scope, b.Enabled, u.ID, s.clk.Now()); err != nil {
		return err
	}
	s.notif.Track(r.Context(), u, "flag_changed", map[string]any{"key": key, "scope": b.Scope, "enabled": b.Enabled})
	return s.getFlags(w, r, u)
}

func (s *Server) demoState(w http.ResponseWriter, r *http.Request, _ *directory.Person) error {
	writeJSON(w, 200, s.demo.State(r.Context()))
	return nil
}

func (s *Server) demoGuard(r *http.Request) error {
	if !s.demo.State(r.Context()).Enabled {
		return apperr.Forbidden("Демо-режим выключен (DEMO_MODE=false).")
	}
	return nil
}

func (s *Server) demoAdvance(w http.ResponseWriter, r *http.Request, _ *directory.Person) error {
	if err := s.demoGuard(r); err != nil {
		return err
	}
	var b struct {
		Days int `json:"days"`
	}
	if err := decode(r, &b); err != nil {
		return err
	}
	if b.Days < 1 || b.Days > 30 {
		return apperr.BadRequest("days от 1 до 30")
	}
	if err := s.demo.Advance(r.Context(), b.Days); err != nil {
		return err
	}
	writeJSON(w, 200, s.demo.State(r.Context()))
	return nil
}

func (s *Server) demoReset(w http.ResponseWriter, r *http.Request, _ *directory.Person) error {
	if err := s.demoGuard(r); err != nil {
		return err
	}
	if err := s.demo.Reset(r.Context()); err != nil {
		return err
	}
	writeJSON(w, 200, s.demo.State(r.Context()))
	return nil
}

func (s *Server) demoFault(w http.ResponseWriter, r *http.Request, _ *directory.Person) error {
	if err := s.demoGuard(r); err != nil {
		return err
	}
	var b struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &b); err != nil {
		return err
	}
	if err := s.demo.SetCorpFault(r.Context(), b.Enabled); err != nil {
		return apperr.Unavailable("Не удалось переключить сбой: %v", err)
	}
	writeJSON(w, 200, s.demo.State(r.Context()))
	return nil
}
