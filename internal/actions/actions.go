// Package actions implements the "plan → confirm → execute → receipt" lifecycle
// for every side effect in corporate services. The buddy only ever *prepares* a
// plan; execution happens after an explicit user confirmation, permissions and
// feature flags are re-checked server-side, the state transition is atomic
// (exactly-once) and the service call carries an idempotency key.
package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"tropa/internal/apperr"
	"tropa/internal/catalog"
	"tropa/internal/clock"
	"tropa/internal/corp"
	"tropa/internal/directory"
	"tropa/internal/domain"
	"tropa/internal/flags"
	"tropa/internal/mcp"
	"tropa/internal/mentor"
	"tropa/internal/notify"
	"tropa/internal/policy"
	"tropa/internal/route"
	"tropa/internal/store"
)

// AbsenceArgs are plan arguments for a day off.
type AbsenceArgs struct {
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
	Comment  string `json:"comment"`
}

// AccessArgs are plan arguments for access requests.
type AccessArgs struct {
	Systems       []string `json:"systems"`
	Justification string   `json:"justification"`
}

// ChecklistArgs are plan arguments for completing a checklist item.
type ChecklistArgs struct {
	InternID string `json:"intern_id"`
	TaskID   string `json:"task_id"`
	Evidence string `json:"evidence"`
}

// QuestionArgs are plan arguments for a mentor question.
type QuestionArgs struct {
	Text    string `json:"text"`
	Context string `json:"context"`
	Urgent  bool   `json:"urgent"`
}

// AccessResult is the stored result of an access action.
type AccessResult struct {
	Requests []corp.AccessRequest `json:"requests"`
}

// QuestionResult is the stored result of a mentor question action.
type QuestionResult struct {
	QuestionID string `json:"question_id"`
	Merged     bool   `json:"merged"`
	MentorName string `json:"mentor_name"`
}

// Service runs the action lifecycle.
type Service struct {
	st     *store.Store
	clk    clock.Clock
	dir    *directory.Directory
	cat    *catalog.Catalog
	pol    *policy.Policy
	corp   corp.Services
	notif  *notify.Notifier
	flags  *flags.Service
	mentor *mentor.Service
	route  *route.Engine
	log    *slog.Logger
	// CallTimeout bounds each corporate call.
	CallTimeout time.Duration
}

// New creates the service.
func New(st *store.Store, clk clock.Clock, dir *directory.Directory, cat *catalog.Catalog, pol *policy.Policy, cs corp.Services,
	n *notify.Notifier, fl *flags.Service, m *mentor.Service, r *route.Engine, log *slog.Logger) *Service {
	return &Service{st: st, clk: clk, dir: dir, cat: cat, pol: pol, corp: cs, notif: n, flags: fl, mentor: m, route: r, log: log, CallTimeout: 10 * time.Second}
}

func (s *Service) insert(ctx context.Context, u *directory.Person, typ, title string, args any, view domain.ActionView) (domain.Action, error) {
	b, _ := json.Marshal(args)
	now := s.clk.Now()
	a := domain.Action{ID: uuid.NewString(), UserID: u.ID, Type: typ, Status: domain.ActionPlanned, Title: title, Args: b, View: view, CreatedAt: now, UpdatedAt: now}
	if err := s.st.InsertAction(ctx, a); err != nil {
		return a, err
	}
	s.notif.Track(ctx, u, "action_planned", map[string]any{"type": typ})
	return a, nil
}

// FormatRange renders a date range in Russian.
func FormatRange(from, to time.Time) string {
	if from.Equal(to) {
		return route.FormatDate(from)
	}
	return route.FormatDate(from) + " — " + route.FormatDate(to)
}

// ---- absence ----

// AbsenceCheck validates dates and collects warnings (calendar conflicts, rules).
func (s *Service) AbsenceCheck(ctx context.Context, u *directory.Person, from, to time.Time) ([]string, error) {
	now := s.clk.Now()
	from, to = clock.StartOfDay(from), clock.StartOfDay(to)
	if to.Before(from) {
		return nil, apperr.BadRequest("Дата окончания раньше даты начала.")
	}
	if from.Before(clock.StartOfDay(now)) {
		return nil, apperr.BadRequest("Эта дата уже прошла — отгул задним числом не оформить.")
	}
	if to.Sub(from) > 48*time.Hour {
		return nil, apperr.BadRequest("Отгул оформляется максимум на 3 дня подряд (тестовое правило). Для более длинного отсутствия нужен отпуск — это через HR.")
	}
	var warnings []string
	weekend := true
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			weekend = false
		}
	}
	if weekend {
		return nil, apperr.BadRequest("Это выходные — отгул не нужен.")
	}
	if clock.WorkdaysBetween(now, from) < 2 {
		warnings = append(warnings, "До отгула меньше 2 рабочих дней — по тестовому правилу так оформлять не принято. Отправить можно, но лучше лично предупредить руководителя.")
	}
	ev, err := s.corp.TeamEvents(ctx, u.ID, u.TeamID, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		warnings = append(warnings, "Не удалось проверить календарь команды — сервис недоступен.")
	} else {
		for _, e := range ev.Events {
			if e.Importance == "high" {
				warnings = append(warnings, fmt.Sprintf("%s в %s — %s. Стоит заранее обсудить отгул с наставником.", e.Title, e.Time, strings.ToLower(e.Note)))
			}
		}
		for d, n := range ev.AbsentByDate {
			if n > 0 {
				warnings = append(warnings, fmt.Sprintf("%s в команде уже отсутствует человек: %d.", d, n))
			}
		}
		slices.Sort(warnings)
	}
	return warnings, nil
}

// PlanAbsence prepares a day-off request.
func (s *Service) PlanAbsence(ctx context.Context, u *directory.Person, from, to time.Time, comment string) (domain.Action, error) {
	title, args, view, err := s.buildAbsence(ctx, u, from, to, comment)
	if err != nil {
		return domain.Action{}, err
	}
	return s.insert(ctx, u, domain.ActionAbsence, title, args, view)
}

func (s *Service) buildAbsence(ctx context.Context, u *directory.Person, from, to time.Time, comment string) (string, AbsenceArgs, domain.ActionView, error) {
	if !s.flags.Enabled(ctx, flags.ActionsAbsence, u.TeamID) {
		return "", AbsenceArgs{}, domain.ActionView{}, apperr.Forbidden("Оформление отгула через Тропу в вашей команде выключено.")
	}
	warnings, err := s.AbsenceCheck(ctx, u, from, to)
	if err != nil {
		return "", AbsenceArgs{}, domain.ActionView{}, err
	}
	comment = strings.TrimSpace(comment)
	if comment == "" {
		comment = "Отгул по личным делам"
	}
	if len([]rune(comment)) > 300 {
		return "", AbsenceArgs{}, domain.ActionView{}, apperr.BadRequest("Комментарий слишком длинный (максимум 300 символов).")
	}
	approver := "руководитель команды"
	if l, ok := s.dir.LeadOf(u.TeamID); ok {
		approver = l.Name
	}
	fields := []domain.Field{fld("Тип", "Отгул"), fld("Даты", FormatRange(from, to)), fld("Комментарий", comment), fld("Согласует", approver)}
	if m, ok := s.dir.ByID(u.MentorID); ok {
		fields = append(fields, domain.Field{Label: "Уведомим", Value: m.Name + " (наставник)"})
	}
	view := domain.ActionView{Service: "Сервис отсутствий", Fields: fields, Warnings: warnings, ConfirmLabel: "Отправить заявку",
		Approver: approver, Permission: "Подаётся от вашего имени, только за себя"}
	args := AbsenceArgs{DateFrom: from.Format("2006-01-02"), DateTo: to.Format("2006-01-02"), Comment: comment}
	return "Отгул: " + FormatRange(from, to), args, view, nil
}

// ---- access ----

// AccessPlanResult explains what can and cannot be requested.
type AccessPlanResult struct {
	Action    *domain.Action
	Forbidden []string
	Granted   []string
	Pending   []string
}

// PlanAccess prepares access requests for systems (filters forbidden/existing).
func (s *Service) PlanAccess(ctx context.Context, u *directory.Person, systems []string, justification string) (AccessPlanResult, error) {
	var res AccessPlanResult
	if !s.flags.Enabled(ctx, flags.ActionsAccess, u.TeamID) {
		return res, apperr.Forbidden("Заявки на доступы через Тропу в вашей команде выключены.")
	}
	have, err := s.st.AccessStates(ctx, u.ID)
	if err != nil {
		return res, err
	}
	var allowed []catalog.AccessSystem
	for _, id := range systems {
		sys, ok := s.cat.AccessSystem(id)
		if !ok {
			continue
		}
		if d := s.pol.CanRequestAccess(u, sys); !d.Allowed {
			res.Forbidden = append(res.Forbidden, d.Reason)
			continue
		}
		switch have[id].Status {
		case domain.AccessGranted:
			res.Granted = append(res.Granted, sys.Title)
			continue
		case domain.AccessPending:
			res.Pending = append(res.Pending, sys.Title)
			continue
		}
		allowed = append(allowed, sys)
	}
	if len(allowed) == 0 {
		return res, nil
	}
	approver := "руководитель команды"
	if l, ok := s.dir.LeadOf(u.TeamID); ok {
		approver = l.Name
	}
	if justification == "" {
		justification = "Онбординг стажёра: доступы по маршруту адаптации"
	}
	var ids, auto, manual []string
	for _, sys := range allowed {
		ids = append(ids, sys.ID)
		label := fmt.Sprintf("%s (%s)", sys.Title, sys.Level)
		if sys.Approval == "auto" {
			auto = append(auto, label)
		} else {
			manual = append(manual, label)
		}
	}
	var fields []domain.Field
	if len(auto) > 0 {
		fields = append(fields, domain.Field{Label: "Выдадут сразу", Value: strings.Join(auto, ", ")})
	}
	if len(manual) > 0 {
		fields = append(fields, domain.Field{Label: "Нужно согласование", Value: strings.Join(manual, ", ") + " — согласует " + approver})
	}
	fields = append(fields, domain.Field{Label: "Обоснование", Value: justification})
	view := domain.ActionView{Service: "Сервис доступов", Fields: fields, ConfirmLabel: "Отправить заявки", Approver: approver,
		Permission: "Только для себя, в пределах роли «" + roleTitle(u.Role) + "»"}
	title := "Доступы: " + strings.Join(titles(allowed), ", ")
	a, err := s.insert(ctx, u, domain.ActionAccess, title, AccessArgs{Systems: ids, Justification: justification}, view)
	if err != nil {
		return res, err
	}
	res.Action = &a
	return res, nil
}

func fld(label, value string) domain.Field { return domain.Field{Label: label, Value: value} }

func titles(ss []catalog.AccessSystem) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Title
	}
	return out
}

func roleTitle(r string) string {
	switch r {
	case directory.RoleIntern:
		return "стажёр"
	case directory.RoleMentor:
		return "наставник"
	case directory.RoleLead:
		return "руководитель"
	}
	return r
}

// ---- checklist ----

// PlanChecklist prepares marking a route task done in the checklist service.
func (s *Service) PlanChecklist(ctx context.Context, actor, intern *directory.Person, taskID, evidence string) (domain.Action, error) {
	r, err := s.route.RouteFor(intern)
	if err != nil {
		return domain.Action{}, err
	}
	t, ok := r.Task(taskID)
	if !ok {
		return domain.Action{}, apperr.NotFound("пункт маршрута не найден")
	}
	if !s.pol.CanCompleteTask(actor, intern.ID, t) && !(t.Kind == "teachback" && actor.ID == intern.ID) {
		return domain.Action{}, apperr.Forbidden("Этот пункт может отметить только его ответственный.")
	}
	if evidence == "" {
		evidence = "Отмечено через Тропу"
	}
	fields := []domain.Field{fld("Пункт", t.Title), fld("Стажёр", intern.Name), fld("Подтверждение", evidence)}
	if t.ChecklistRef != "" {
		fields = append(fields, domain.Field{Label: "В чек-листе", Value: t.ChecklistRef})
	}
	view := domain.ActionView{Service: "Чек-лист новичка", Fields: fields, ConfirmLabel: "Отметить выполненным", Permission: "Стажёр отмечает свои пункты, наставник — свои"}
	return s.insert(ctx, actor, domain.ActionChecklist, "Чек-лист: "+t.Title, ChecklistArgs{InternID: intern.ID, TaskID: t.ID, Evidence: evidence}, view)
}

// ---- mentor question ----

// PlanQuestion prepares a question for the mentor.
func (s *Service) PlanQuestion(ctx context.Context, u *directory.Person, text, contextLine string, urgent bool) (domain.Action, error) {
	if !s.pol.CanAskMentor(u) {
		return domain.Action{}, apperr.Forbidden("Вопросы наставнику доступны стажёрам с назначенным наставником.")
	}
	if !s.flags.Enabled(ctx, flags.MentorDigest, u.TeamID) {
		return domain.Action{}, apperr.Forbidden("Очередь вопросов наставнику в вашей команде выключена.")
	}
	m, _ := s.dir.ByID(u.MentorID)
	when := "в ежедневную сводку — наставник разберёт её в удобное время"
	if urgent {
		when = "сразу, с пометкой «срочно»"
	}
	view := domain.ActionView{Service: "Вопросы наставнику", ConfirmLabel: "Отправить вопрос", Approver: m.Name,
		Fields:     []domain.Field{fld("Кому", m.Name+" (наставник)"), fld("Вопрос", text), fld("Контекст", contextLine), fld("Как", when)},
		Permission: "Наставник увидит только этот вопрос — переписка с Тропой остаётся приватной",
		Note:       "Похожие вопросы других стажёров объединяются — наставник отвечает один раз."}
	return s.insert(ctx, u, domain.ActionQuestion, "Вопрос наставнику", QuestionArgs{Text: text, Context: contextLine, Urgent: urgent}, view)
}

// ---- lifecycle ----

func (s *Service) owned(ctx context.Context, u *directory.Person, id string) (domain.Action, error) {
	a, err := s.st.GetAction(ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && a.UserID != u.ID) {
		return a, apperr.NotFound("действие не найдено")
	}
	return a, err
}

// Get returns an action owned by u.
func (s *Service) Get(ctx context.Context, u *directory.Person, id string) (domain.Action, error) {
	return s.owned(ctx, u, id)
}

// List returns u's actions.
func (s *Service) List(ctx context.Context, u *directory.Person) ([]domain.Action, error) {
	return s.st.ListActions(ctx, u.ID, 50)
}

// Cancel cancels a planned or failed action.
func (s *Service) Cancel(ctx context.Context, u *directory.Person, id string) (domain.Action, error) {
	a, err := s.owned(ctx, u, id)
	if err != nil {
		return a, err
	}
	now := s.clk.Now()
	for _, from := range []string{domain.ActionPlanned, domain.ActionFailed} {
		ok, err := s.st.TransitionAction(ctx, id, from, domain.ActionCancelled, now)
		if err != nil {
			return a, err
		}
		if ok {
			s.notif.Track(ctx, u, "action_cancelled", map[string]any{"type": a.Type})
			return s.st.GetAction(ctx, id)
		}
	}
	return a, apperr.Conflict("Это действие уже нельзя отменить.")
}

// EditAbsence changes dates/comment of a planned day-off request.
func (s *Service) EditAbsence(ctx context.Context, u *directory.Person, id string, from, to time.Time, comment string) (domain.Action, error) {
	a, err := s.owned(ctx, u, id)
	if err != nil {
		return a, err
	}
	if a.Type != domain.ActionAbsence || a.Status != domain.ActionPlanned {
		return a, apperr.Conflict("Изменить можно только неотправленную заявку на отгул.")
	}
	title, args, view, err := s.buildAbsence(ctx, u, from, to, comment)
	if err != nil {
		return a, err
	}
	b, _ := json.Marshal(args)
	if err := s.st.UpdatePlan(ctx, a.ID, title, b, view, s.clk.Now()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return a, apperr.Conflict("Заявка уже отправлена или отменена.")
		}
		return a, err
	}
	return s.st.GetAction(ctx, a.ID)
}

// Confirm executes a planned (or retries a failed) action exactly once.
func (s *Service) Confirm(ctx context.Context, u *directory.Person, id string) (domain.Action, error) {
	a, err := s.owned(ctx, u, id)
	if err != nil {
		return a, err
	}
	now := s.clk.Now()
	ok, err := s.st.TransitionAction(ctx, id, domain.ActionPlanned, domain.ActionExecuting, now)
	if err == nil && !ok {
		ok, err = s.st.TransitionAction(ctx, id, domain.ActionFailed, domain.ActionExecuting, now)
	}
	if err != nil {
		return a, err
	}
	if !ok {
		if a.Status == domain.ActionDone {
			return a, nil // idempotent repeat confirm
		}
		return a, apperr.Conflict("Действие уже выполняется или отменено.")
	}
	cctx, cancel := context.WithTimeout(ctx, s.CallTimeout)
	defer cancel()
	result, extID, extStatus, execErr := s.execute(cctx, u, a)
	now = s.clk.Now()
	if execErr != nil {
		msg := userMessage(execErr)
		_ = s.st.FinishAction(ctx, id, domain.ActionFailed, nil, "", "", msg, now)
		s.notif.Track(ctx, u, "action_failed", map[string]any{"type": a.Type, "error": msg})
		s.log.Warn("action failed", "id", id, "type", a.Type, "err", execErr)
		out, _ := s.st.GetAction(ctx, id)
		return out, nil
	}
	if err := s.st.FinishAction(ctx, id, domain.ActionDone, result, extID, extStatus, "", now); err != nil {
		return a, err
	}
	s.notif.Track(ctx, u, "action_executed", map[string]any{"type": a.Type, "external_status": extStatus})
	return s.st.GetAction(ctx, id)
}

func userMessage(err error) string {
	var te *mcp.ToolError
	if errors.As(err, &te) {
		return te.Message
	}
	if e, ok := apperr.As(err); ok {
		return e.Message
	}
	if errors.Is(err, mcp.ErrCircuitOpen) {
		return "Корпоративный сервис временно недоступен. Заявка не отправлена — попробуйте повторить чуть позже."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Сервис не ответил вовремя. Заявка не отправлена — можно повторить."
	}
	return "Корпоративный сервис недоступен. Заявка не отправлена — можно повторить."
}

func (s *Service) execute(ctx context.Context, u *directory.Person, a domain.Action) (any, string, string, error) {
	switch a.Type {
	case domain.ActionAbsence:
		var args AbsenceArgs
		_ = json.Unmarshal(a.Args, &args)
		if !s.flags.Enabled(ctx, flags.ActionsAbsence, u.TeamID) {
			return nil, "", "", apperr.Forbidden("Оформление отгула через Тропу выключено.")
		}
		r, err := s.corp.CreateAbsence(ctx, u.ID, corp.AbsenceCreate{EmployeeID: u.ID, Type: "day_off", DateFrom: args.DateFrom, DateTo: args.DateTo, Comment: args.Comment, IdempotencyKey: a.ID})
		if err != nil {
			return nil, "", "", err
		}
		if u.MentorID != "" {
			s.notif.Notify(ctx, u.MentorID, "info", u.Name+" оформил отгул", FormatArgsRange(args)+" · согласует "+r.ApproverName, "action:"+a.ID)
		}
		if r.ApproverID != "" {
			s.notif.Notify(ctx, r.ApproverID, "approval", "Заявка на отгул: "+u.Name, FormatArgsRange(args), "action:"+a.ID)
		}
		return r, r.RequestID, r.Status, nil
	case domain.ActionAccess:
		var args AccessArgs
		_ = json.Unmarshal(a.Args, &args)
		if !s.flags.Enabled(ctx, flags.ActionsAccess, u.TeamID) {
			return nil, "", "", apperr.Forbidden("Заявки на доступы через Тропу выключены.")
		}
		var res AccessResult
		for _, sysID := range args.Systems {
			sys, _ := s.cat.AccessSystem(sysID)
			if d := s.pol.CanRequestAccess(u, sys); !d.Allowed {
				return nil, "", "", apperr.Forbidden("%s", d.Reason)
			}
			r, err := s.corp.CreateAccess(ctx, u.ID, corp.AccessCreate{EmployeeID: u.ID, System: sysID, Justification: args.Justification, IdempotencyKey: a.ID + ":" + sysID})
			if err != nil {
				return nil, "", "", err
			}
			res.Requests = append(res.Requests, r)
			if err := s.st.UpsertAccess(ctx, domain.AccessState{UserID: u.ID, System: sysID, Status: r.Status, RequestID: r.RequestID, ActionID: a.ID, UpdatedAt: s.clk.Now()}); err != nil {
				return nil, "", "", err
			}
		}
		ext := accessStatus(res.Requests)
		if ext == domain.ExtPendingApproval || ext == domain.ExtPartial {
			if l, ok := s.dir.LeadOf(u.TeamID); ok {
				s.notif.Notify(ctx, l.ID, "approval", "Заявка на доступ: "+u.Name, strings.Join(pendingSystems(res.Requests), ", "), "action:"+a.ID)
			}
		}
		s.syncAccessTasks(ctx, u)
		return res, firstID(res.Requests), ext, nil
	case domain.ActionChecklist:
		var args ChecklistArgs
		_ = json.Unmarshal(a.Args, &args)
		intern, ok := s.dir.ByID(args.InternID)
		if !ok {
			return nil, "", "", apperr.NotFound("стажёр не найден")
		}
		r, err := s.route.RouteFor(intern)
		if err != nil {
			return nil, "", "", err
		}
		t, ok := r.Task(args.TaskID)
		if !ok {
			return nil, "", "", apperr.NotFound("пункт не найден")
		}
		if !s.pol.CanCompleteTask(u, intern.ID, t) && !(t.Kind == "teachback" && u.ID == intern.ID) {
			return nil, "", "", apperr.Forbidden("Нет прав отметить этот пункт.")
		}
		item, err := s.corp.CompleteChecklist(ctx, u.ID, corp.ChecklistComplete{EmployeeID: intern.ID, ItemID: t.ID, Evidence: args.Evidence, IdempotencyKey: a.ID})
		if err != nil {
			return nil, "", "", err
		}
		if err := s.st.SetTaskStatus(ctx, intern.ID, t.ID, domain.TaskDone, s.clk.Now()); err != nil {
			return nil, "", "", err
		}
		if u.ID != intern.ID {
			s.notif.Notify(ctx, intern.ID, "task", u.Name+" отметил: «"+t.Title+"»", "Пункт выполнен — проверь, что открылось в маршруте.", "task:"+t.ID)
		}
		s.notif.Track(ctx, intern, "task_done", map[string]any{"task": t.ID, "by": u.Role, "kind": t.Kind})
		return item, item.ItemID, domain.ExtCompleted, nil
	case domain.ActionQuestion:
		var args QuestionArgs
		_ = json.Unmarshal(a.Args, &args)
		q, merged, err := s.mentor.Ask(ctx, u, args.Text, args.Context, args.Urgent)
		if err != nil {
			return nil, "", "", err
		}
		name := ""
		if m, ok := s.dir.ByID(q.MentorID); ok {
			name = m.Name
		}
		return QuestionResult{QuestionID: q.ID, Merged: merged, MentorName: name}, q.ID, domain.ExtOpen, nil
	}
	return nil, "", "", apperr.BadRequest("неизвестный тип действия")
}

// FormatArgsRange renders AbsenceArgs dates.
func FormatArgsRange(a AbsenceArgs) string {
	from, _ := time.ParseInLocation("2006-01-02", a.DateFrom, clock.MSK)
	to, _ := time.ParseInLocation("2006-01-02", a.DateTo, clock.MSK)
	return FormatRange(from, to)
}

func firstID(rs []corp.AccessRequest) string {
	if len(rs) == 0 {
		return ""
	}
	return rs[0].RequestID
}

func pendingSystems(rs []corp.AccessRequest) []string {
	var out []string
	for _, r := range rs {
		if r.Status == domain.ExtPendingApproval {
			out = append(out, r.System)
		}
	}
	return out
}

func accessStatus(rs []corp.AccessRequest) string {
	granted, pending := 0, 0
	for _, r := range rs {
		switch r.Status {
		case domain.ExtGranted:
			granted++
		case domain.ExtPendingApproval:
			pending++
		}
	}
	switch {
	case pending > 0 && granted > 0:
		return domain.ExtPartial
	case pending > 0:
		return domain.ExtPendingApproval
	case granted == len(rs):
		return domain.ExtGranted
	case granted == 0:
		return domain.ExtRejected
	}
	return domain.ExtPartial
}

// syncAccessTasks persists route statuses of access tasks from the access mirror.
func (s *Service) syncAccessTasks(ctx context.Context, intern *directory.Person) {
	r, err := s.route.RouteFor(intern)
	if err != nil {
		return
	}
	have, err := s.st.AccessStates(ctx, intern.ID)
	if err != nil {
		return
	}
	states, _ := s.st.TaskStates(ctx, intern.ID)
	now := s.clk.Now()
	for _, t := range r.AllTasks() {
		if t.Kind != "access" {
			continue
		}
		granted, pending := 0, 0
		for _, sys := range t.Systems {
			switch have[sys].Status {
			case domain.AccessGranted:
				granted++
			case domain.AccessPending:
				pending++
			}
		}
		status := ""
		switch {
		case granted == len(t.Systems):
			status = domain.TaskDone
		case pending > 0:
			status = domain.TaskWaiting
		}
		if status != "" && states[t.ID].Status != status {
			_ = s.st.SetTaskStatus(ctx, intern.ID, t.ID, status, now)
			if status == domain.TaskDone {
				s.notif.Track(ctx, intern, "task_done", map[string]any{"task": t.ID, "by": "system", "kind": "access"})
			}
		}
	}
}

// ---- approvals (lead) ----

// Approval is a pending request seen by a lead.
type Approval struct {
	Action    domain.Action `json:"action"`
	Requester string        `json:"requester"`
	Kind      string        `json:"kind"`
	Details   string        `json:"details"`
}

// PendingApprovals lists requests of the lead's team awaiting a decision.
func (s *Service) PendingApprovals(ctx context.Context, lead *directory.Person) ([]Approval, error) {
	if lead.Role != directory.RoleLead {
		return nil, apperr.Forbidden("согласования доступны руководителю команды")
	}
	var ids []string
	for _, p := range s.dir.All() {
		if p.TeamID == lead.TeamID && p.ID != lead.ID {
			ids = append(ids, p.ID)
		}
	}
	var out []Approval
	for _, ext := range []string{domain.ExtPendingApproval, domain.ExtPartial} {
		as, err := s.st.ListActionsForUsers(ctx, ids, []string{domain.ActionAbsence, domain.ActionAccess}, ext)
		if err != nil {
			return nil, err
		}
		for _, a := range as {
			p, _ := s.dir.ByID(a.UserID)
			ap := Approval{Action: a, Requester: p.Name}
			switch a.Type {
			case domain.ActionAbsence:
				var args AbsenceArgs
				_ = json.Unmarshal(a.Args, &args)
				ap.Kind, ap.Details = "Отгул", FormatArgsRange(args)+" · "+args.Comment
			case domain.ActionAccess:
				var res AccessResult
				_ = json.Unmarshal(a.Result, &res)
				ap.Kind, ap.Details = "Доступ", strings.Join(pendingSystems(res.Requests), ", ")
			}
			out = append(out, ap)
		}
	}
	return out, nil
}

// Decide approves or rejects a pending request (lead only; re-checked by the service too).
func (s *Service) Decide(ctx context.Context, lead *directory.Person, id string, approve bool, comment string) (domain.Action, error) {
	a, err := s.st.GetAction(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return a, apperr.NotFound("заявка не найдена")
	}
	if err != nil {
		return a, err
	}
	if !s.pol.CanApprove(lead, a.UserID) {
		return a, apperr.Forbidden("Согласовать может только руководитель команды сотрудника.")
	}
	if a.Status != domain.ActionDone || (a.ExternalStatus != domain.ExtPendingApproval && a.ExternalStatus != domain.ExtPartial) {
		return a, apperr.Conflict("Заявка уже рассмотрена.")
	}
	cctx, cancel := context.WithTimeout(ctx, s.CallTimeout)
	defer cancel()
	requester, _ := s.dir.ByID(a.UserID)
	switch a.Type {
	case domain.ActionAbsence:
		r, err := s.corp.DecideAbsence(cctx, lead.ID, a.ExternalID, approve, comment)
		if err != nil {
			return a, apperr.Unavailable("%s", userMessage(err))
		}
		if err := s.st.SetExternalStatus(ctx, a.ID, r.Status, r, s.clk.Now()); err != nil {
			return a, err
		}
	case domain.ActionAccess:
		var res AccessResult
		_ = json.Unmarshal(a.Result, &res)
		for i, r := range res.Requests {
			if r.Status != domain.ExtPendingApproval {
				continue
			}
			nr, err := s.corp.DecideAccess(cctx, lead.ID, r.RequestID, approve, comment)
			if err != nil {
				return a, apperr.Unavailable("%s", userMessage(err))
			}
			res.Requests[i] = nr
			_ = s.st.UpsertAccess(ctx, domain.AccessState{UserID: a.UserID, System: nr.System, Status: nr.Status, RequestID: nr.RequestID, ActionID: a.ID, UpdatedAt: s.clk.Now()})
			s.trackWait(ctx, requester, a, nr)
		}
		if err := s.st.SetExternalStatus(ctx, a.ID, accessStatus(res.Requests), res, s.clk.Now()); err != nil {
			return a, err
		}
		s.syncAccessTasks(ctx, requester)
	}
	s.notifyDecision(ctx, requester, a, approve)
	s.notif.Track(ctx, lead, "approval_decided", map[string]any{"type": a.Type, "approve": approve})
	return s.st.GetAction(ctx, a.ID)
}

// trackWait records how long a non-standard access waited for approval.
func (s *Service) trackWait(ctx context.Context, u *directory.Person, a domain.Action, r corp.AccessRequest) {
	if r.Status != domain.ExtGranted {
		return
	}
	hours := s.clk.Now().Sub(a.CreatedAt).Hours()
	s.notif.Track(ctx, u, "access_wait", map[string]any{"system": r.System, "hours": float64(int(hours*10)) / 10})
}

func (s *Service) notifyDecision(ctx context.Context, u *directory.Person, a domain.Action, approve bool) {
	verb := "согласована"
	if !approve {
		verb = "отклонена"
	}
	s.notif.Notify(ctx, u.ID, "status", "Заявка "+verb+": "+a.Title, "Статус обновлён в карточке заявки.", "action:"+a.ID)
}

// Sync polls corporate services for decisions on pending requests (AI-cron job).
func (s *Service) Sync(ctx context.Context) (int, error) {
	pending, err := s.st.PendingExternal(ctx)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, a := range pending {
		u, ok := s.dir.ByID(a.UserID)
		if !ok {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, s.CallTimeout)
		switch a.Type {
		case domain.ActionAbsence:
			r, err := s.corp.GetAbsence(cctx, u.ID, a.ExternalID)
			if err == nil && r.Status != a.ExternalStatus {
				_ = s.st.SetExternalStatus(ctx, a.ID, r.Status, r, s.clk.Now())
				s.notifyDecision(ctx, u, a, r.Status == domain.ExtApproved)
				changed++
			}
		case domain.ActionAccess:
			var res AccessResult
			_ = json.Unmarshal(a.Result, &res)
			dirty := false
			for i, r := range res.Requests {
				if r.Status != domain.ExtPendingApproval {
					continue
				}
				nr, err := s.corp.GetAccess(cctx, u.ID, r.RequestID)
				if err == nil && nr.Status != r.Status {
					res.Requests[i], dirty = nr, true
					_ = s.st.UpsertAccess(ctx, domain.AccessState{UserID: u.ID, System: nr.System, Status: nr.Status, RequestID: nr.RequestID, ActionID: a.ID, UpdatedAt: s.clk.Now()})
					s.trackWait(ctx, u, a, nr)
				}
			}
			if dirty {
				ext := accessStatus(res.Requests)
				_ = s.st.SetExternalStatus(ctx, a.ID, ext, res, s.clk.Now())
				s.syncAccessTasks(ctx, u)
				if ext != domain.ExtPendingApproval && ext != domain.ExtPartial {
					s.notifyDecision(ctx, u, a, ext == domain.ExtGranted)
				}
				changed++
			}
		}
		cancel()
	}
	return changed, nil
}
