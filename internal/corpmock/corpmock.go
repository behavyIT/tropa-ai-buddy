// Package corpmock imitates Avito corporate services (absences, access,
// newcomer checklist, team calendar) behind an MCP server. It enforces its own
// permissions on the delegated identity (X-On-Behalf-Of) — defence in depth:
// even a buggy agent cannot approve its own request.
package corpmock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"tropa/internal/catalog"
	"tropa/internal/clock"
	"tropa/internal/corp"
	"tropa/internal/directory"
	"tropa/internal/mcp"
)

type state struct {
	Seq       int                                       `json:"seq"`
	Absences  map[string]*corp.AbsenceRequest           `json:"absences"`
	Access    map[string]*corp.AccessRequest            `json:"access"`
	Checklist map[string]map[string]*corp.ChecklistItem `json:"checklist"`
	Idem      map[string]string                         `json:"idem"`
}

func newState() state {
	return state{Absences: map[string]*corp.AbsenceRequest{}, Access: map[string]*corp.AccessRequest{},
		Checklist: map[string]map[string]*corp.ChecklistItem{}, Idem: map[string]string{}}
}

// Mock is the corporate services imitation.
type Mock struct {
	mu        sync.Mutex
	st        state
	dir       *directory.Directory
	cat       *catalog.Catalog
	now       func() time.Time
	fault     bool
	Latency   time.Duration
	StatePath string
	Server    *mcp.Server
}

// New creates the mock and registers its MCP tools.
func New(dir *directory.Directory, cat *catalog.Catalog, token string, now func() time.Time) *Mock {
	if now == nil {
		now = func() time.Time { return time.Now().In(clock.MSK) }
	}
	m := &Mock{st: newState(), dir: dir, cat: cat, now: now}
	m.Server = mcp.NewServer("avito-corp-services-mock", "1.0.0", token)
	m.Server.Before = m.before
	m.register()
	return m
}

func (m *Mock) before(ctx context.Context) error {
	m.mu.Lock()
	fault, lat := m.fault, m.Latency
	m.mu.Unlock()
	if fault {
		return mcp.ErrUnavailable
	}
	if lat > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(lat):
		}
	}
	return nil
}

// SetFault toggles a simulated outage.
func (m *Mock) SetFault(on bool) { m.mu.Lock(); m.fault = on; m.mu.Unlock() }

// Fault reports the outage flag.
func (m *Mock) Fault() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.fault }

// Reset clears all state.
func (m *Mock) Reset() {
	m.mu.Lock()
	m.st, m.fault = newState(), false
	m.mu.Unlock()
	m.save()
}

// Load restores state from StatePath if present.
func (m *Mock) Load() error {
	if m.StatePath == "" {
		return nil
	}
	b, err := os.ReadFile(m.StatePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	st := newState()
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	m.mu.Lock()
	m.st = st
	m.mu.Unlock()
	return nil
}

func (m *Mock) save() {
	if m.StatePath == "" {
		return
	}
	m.mu.Lock()
	b, _ := json.Marshal(m.st)
	m.mu.Unlock()
	_ = os.WriteFile(m.StatePath, b, 0o600)
}

func (m *Mock) nextID(prefix string) string {
	m.st.Seq++
	return fmt.Sprintf("%s-%05d", prefix, m.st.Seq)
}

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	dec := json.NewDecoder(bytesReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, mcp.Errorf("invalid_arguments", "некорректные аргументы: %v", err)
	}
	return v, nil
}

func (m *Mock) person(id string) (*directory.Person, error) {
	p, ok := m.dir.ByID(id)
	if !ok {
		return nil, mcp.Errorf("unknown_employee", "сотрудник %s не найден", id)
	}
	return p, nil
}

func (m *Mock) register() {
	write := mcp.Annotations{ReadOnlyHint: false, IdempotentHint: true}
	read := mcp.Annotations{ReadOnlyHint: true, IdempotentHint: true}
	m.Server.Register(mcp.Tool{Info: mcp.ToolInfo{Name: corp.ToolAbsenceCreate, Title: "Создать заявку на отгул",
		Description: "Создаёт заявку на отгул в сервисе отсутствий от имени сотрудника. Требует подтверждения пользователя.",
		InputSchema: obj(map[string]any{"employee_id": str("ID сотрудника"), "type": map[string]any{"type": "string", "enum": []string{"day_off"}},
			"date_from": str("YYYY-MM-DD"), "date_to": str("YYYY-MM-DD"), "comment": str("Комментарий"), "idempotency_key": str("Ключ идемпотентности")},
			"employee_id", "type", "date_from", "date_to", "idempotency_key"), Annotations: write}, Handler: m.absenceCreate})
	m.Server.Register(mcp.Tool{Info: mcp.ToolInfo{Name: corp.ToolAbsenceGet, Title: "Статус заявки на отгул", Description: "Возвращает заявку на отгул.",
		InputSchema: obj(map[string]any{"request_id": str("ID заявки")}, "request_id"), Annotations: read}, Handler: m.absenceGet})
	m.Server.Register(mcp.Tool{Info: mcp.ToolInfo{Name: corp.ToolAbsenceDecide, Title: "Согласовать отгул", Description: "Согласует или отклоняет заявку (только руководитель команды).",
		InputSchema: obj(map[string]any{"request_id": str("ID заявки"), "decision": map[string]any{"type": "string", "enum": []string{"approve", "reject"}}, "comment": str("Комментарий")}, "request_id", "decision"),
		Annotations: mcp.Annotations{ReadOnlyHint: false, DestructiveHint: false}}, Handler: m.absenceDecide})
	m.Server.Register(mcp.Tool{Info: mcp.ToolInfo{Name: corp.ToolCalendarEvents, Title: "Календарь команды", Description: "События команды и число отсутствующих по датам (без имён).",
		InputSchema: obj(map[string]any{"team_id": str("ID команды"), "date_from": str("YYYY-MM-DD"), "date_to": str("YYYY-MM-DD")}, "team_id", "date_from", "date_to"), Annotations: read}, Handler: m.calendar})
	m.Server.Register(mcp.Tool{Info: mcp.ToolInfo{Name: corp.ToolAccessCatalog, Title: "Каталог доступов", Description: "Системы, уровни и порядок согласования.",
		InputSchema: obj(map[string]any{}), Annotations: read}, Handler: m.accessCatalog})
	m.Server.Register(mcp.Tool{Info: mcp.ToolInfo{Name: corp.ToolAccessCreate, Title: "Запросить доступ", Description: "Создаёт заявку на доступ. Стандартные выдаются сразу, остальные — после согласования руководителем.",
		InputSchema: obj(map[string]any{"employee_id": str("ID сотрудника"), "system": str("ID системы из каталога"), "justification": str("Обоснование"), "idempotency_key": str("Ключ идемпотентности")},
			"employee_id", "system", "idempotency_key"), Annotations: write}, Handler: m.accessCreate})
	m.Server.Register(mcp.Tool{Info: mcp.ToolInfo{Name: corp.ToolAccessGet, Title: "Статус заявки на доступ", Description: "Возвращает заявку на доступ.",
		InputSchema: obj(map[string]any{"request_id": str("ID заявки")}, "request_id"), Annotations: read}, Handler: m.accessGet})
	m.Server.Register(mcp.Tool{Info: mcp.ToolInfo{Name: corp.ToolAccessDecide, Title: "Согласовать доступ", Description: "Согласует или отклоняет доступ (только руководитель команды).",
		InputSchema: obj(map[string]any{"request_id": str("ID заявки"), "decision": map[string]any{"type": "string", "enum": []string{"approve", "reject"}}, "comment": str("Комментарий")}, "request_id", "decision"),
		Annotations: mcp.Annotations{ReadOnlyHint: false}}, Handler: m.accessDecide})
	m.Server.Register(mcp.Tool{Info: mcp.ToolInfo{Name: corp.ToolChecklistComplete, Title: "Отметить пункт чек-листа", Description: "Отмечает пункт чек-листа новичка выполненным.",
		InputSchema: obj(map[string]any{"employee_id": str("ID стажёра"), "item_id": str("ID пункта"), "evidence": str("Подтверждение"), "idempotency_key": str("Ключ идемпотентности")},
			"employee_id", "item_id", "idempotency_key"), Annotations: write}, Handler: m.checklistComplete})
	m.Server.Register(mcp.Tool{Info: mcp.ToolInfo{Name: corp.ToolChecklistGet, Title: "Чек-лист новичка", Description: "Пункты чек-листа сотрудника.",
		InputSchema: obj(map[string]any{"employee_id": str("ID стажёра")}, "employee_id"), Annotations: read}, Handler: m.checklistGet})
}

func parseDate(s string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", s, clock.MSK)
	if err != nil {
		return t, mcp.Errorf("invalid_date", "некорректная дата %q", s)
	}
	return t, nil
}

func (m *Mock) absenceCreate(_ context.Context, cc mcp.CallContext, raw json.RawMessage) (any, error) {
	a, err := decode[corp.AbsenceCreate](raw)
	if err != nil {
		return nil, err
	}
	if cc.OnBehalfOf != a.EmployeeID {
		return nil, mcp.Errorf("forbidden", "заявку на отгул можно подать только за себя")
	}
	emp, err := m.person(a.EmployeeID)
	if err != nil {
		return nil, err
	}
	from, err := parseDate(a.DateFrom)
	if err != nil {
		return nil, err
	}
	to, err := parseDate(a.DateTo)
	if err != nil {
		return nil, err
	}
	if a.Type != "day_off" {
		return nil, mcp.Errorf("unsupported_type", "поддерживается только тип day_off")
	}
	if to.Before(from) {
		return nil, mcp.Errorf("invalid_range", "дата окончания раньше даты начала")
	}
	if to.Sub(from) > 2*24*time.Hour {
		return nil, mcp.Errorf("too_long", "отгул оформляется максимум на 3 дня подряд (тестовое правило)")
	}
	if from.Before(clock.StartOfDay(m.now())) {
		return nil, mcp.Errorf("in_past", "нельзя оформить отгул задним числом")
	}
	m.mu.Lock()
	if id, ok := m.st.Idem[a.IdempotencyKey]; ok && a.IdempotencyKey != "" {
		r := *m.st.Absences[id]
		m.mu.Unlock()
		return r, nil
	}
	lead, _ := m.dir.LeadOf(emp.TeamID)
	r := &corp.AbsenceRequest{RequestID: m.nextID("ABS"), EmployeeID: emp.ID, Type: a.Type, DateFrom: a.DateFrom, DateTo: a.DateTo, Comment: a.Comment,
		Status: "pending_approval", CreatedAt: m.now().Format(time.RFC3339)}
	if lead != nil {
		r.ApproverID, r.ApproverName = lead.ID, lead.Name
	}
	m.st.Absences[r.RequestID] = r
	m.st.Idem[a.IdempotencyKey] = r.RequestID
	out := *r
	m.mu.Unlock()
	m.save()
	return out, nil
}

func (m *Mock) canRead(actor, owner, approver string) bool {
	if actor == owner || actor == approver {
		return true
	}
	p, ok := m.dir.ByID(actor)
	return ok && p.Role == directory.RoleHRBP
}

func (m *Mock) absenceGet(_ context.Context, cc mcp.CallContext, raw json.RawMessage) (any, error) {
	a, err := decode[struct {
		RequestID string `json:"request_id"`
	}](raw)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.st.Absences[a.RequestID]
	if !ok {
		return nil, mcp.Errorf("not_found", "заявка %s не найдена", a.RequestID)
	}
	if !m.canRead(cc.OnBehalfOf, r.EmployeeID, r.ApproverID) {
		return nil, mcp.Errorf("forbidden", "нет прав на просмотр заявки")
	}
	return *r, nil
}

type decideArgs struct {
	RequestID string `json:"request_id"`
	Decision  string `json:"decision"`
	Comment   string `json:"comment"`
}

func (m *Mock) checkLead(actor, employeeID string) error {
	p, ok := m.dir.ByID(actor)
	e, ok2 := m.dir.ByID(employeeID)
	if !ok || !ok2 || p.Role != directory.RoleLead || p.TeamID != e.TeamID || p.ID == e.ID {
		return mcp.Errorf("forbidden", "согласовать может только руководитель команды сотрудника")
	}
	return nil
}

func (m *Mock) absenceDecide(_ context.Context, cc mcp.CallContext, raw json.RawMessage) (any, error) {
	a, err := decode[decideArgs](raw)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	r, ok := m.st.Absences[a.RequestID]
	if !ok {
		m.mu.Unlock()
		return nil, mcp.Errorf("not_found", "заявка %s не найдена", a.RequestID)
	}
	if err := m.checkLead(cc.OnBehalfOf, r.EmployeeID); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if r.Status != "pending_approval" {
		out := *r
		m.mu.Unlock()
		return out, nil // idempotent: decision already made
	}
	switch a.Decision {
	case "approve":
		r.Status = "approved"
	case "reject":
		r.Status = "rejected"
	default:
		m.mu.Unlock()
		return nil, mcp.Errorf("invalid_arguments", "decision должен быть approve или reject")
	}
	r.DecisionComment = a.Comment
	out := *r
	m.mu.Unlock()
	m.save()
	return out, nil
}

func (m *Mock) calendar(_ context.Context, cc mcp.CallContext, raw json.RawMessage) (any, error) {
	a, err := decode[struct {
		TeamID   string `json:"team_id"`
		DateFrom string `json:"date_from"`
		DateTo   string `json:"date_to"`
	}](raw)
	if err != nil {
		return nil, err
	}
	actor, ok := m.dir.ByID(cc.OnBehalfOf)
	if !ok || (actor.TeamID != a.TeamID && actor.Role != directory.RoleHRBP) {
		return nil, mcp.Errorf("forbidden", "календарь доступен только участникам команды")
	}
	team, ok := m.cat.Teams[a.TeamID]
	if !ok {
		return nil, mcp.Errorf("not_found", "команда не найдена")
	}
	out := corp.TeamEvents{Events: []catalog.CalendarEvent{}, AbsentByDate: map[string]int{}}
	for _, e := range team.Calendar {
		if e.Date >= a.DateFrom && e.Date <= a.DateTo {
			out.Events = append(out.Events, e)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.st.Absences {
		e, ok := m.dir.ByID(r.EmployeeID)
		if !ok || e.TeamID != a.TeamID || r.Status == "rejected" || r.EmployeeID == actor.ID {
			continue
		}
		from, _ := parseDate(r.DateFrom)
		to, _ := parseDate(r.DateTo)
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			ds := d.Format("2006-01-02")
			if ds >= a.DateFrom && ds <= a.DateTo {
				out.AbsentByDate[ds]++
			}
		}
	}
	return out, nil
}

func (m *Mock) accessCatalog(context.Context, mcp.CallContext, json.RawMessage) (any, error) {
	return map[string]any{"systems": m.cat.Access}, nil
}

func (m *Mock) accessCreate(_ context.Context, cc mcp.CallContext, raw json.RawMessage) (any, error) {
	a, err := decode[corp.AccessCreate](raw)
	if err != nil {
		return nil, err
	}
	if cc.OnBehalfOf != a.EmployeeID {
		return nil, mcp.Errorf("forbidden", "доступ можно запросить только для себя")
	}
	emp, err := m.person(a.EmployeeID)
	if err != nil {
		return nil, err
	}
	sys, ok := m.cat.AccessSystem(a.System)
	if !ok {
		return nil, mcp.Errorf("unknown_system", "система %s отсутствует в каталоге", a.System)
	}
	if slices.Contains(sys.ForbiddenFor, emp.Role) {
		return nil, mcp.Errorf("forbidden", "%s недоступен для вашей роли", sys.Title)
	}
	m.mu.Lock()
	if id, ok := m.st.Idem[a.IdempotencyKey]; ok && a.IdempotencyKey != "" {
		r := *m.st.Access[id]
		m.mu.Unlock()
		return r, nil
	}
	r := &corp.AccessRequest{RequestID: m.nextID("ACC"), EmployeeID: emp.ID, System: sys.ID, Level: sys.Level, CreatedAt: m.now().Format(time.RFC3339)}
	if sys.Approval == "auto" {
		r.Status = "granted"
	} else {
		r.Status = "pending_approval"
		if lead, ok := m.dir.LeadOf(emp.TeamID); ok {
			r.ApproverID, r.ApproverName = lead.ID, lead.Name
		}
	}
	m.st.Access[r.RequestID] = r
	m.st.Idem[a.IdempotencyKey] = r.RequestID
	out := *r
	m.mu.Unlock()
	m.save()
	return out, nil
}

func (m *Mock) accessGet(_ context.Context, cc mcp.CallContext, raw json.RawMessage) (any, error) {
	a, err := decode[struct {
		RequestID string `json:"request_id"`
	}](raw)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.st.Access[a.RequestID]
	if !ok {
		return nil, mcp.Errorf("not_found", "заявка %s не найдена", a.RequestID)
	}
	if !m.canRead(cc.OnBehalfOf, r.EmployeeID, r.ApproverID) {
		return nil, mcp.Errorf("forbidden", "нет прав на просмотр заявки")
	}
	return *r, nil
}

func (m *Mock) accessDecide(_ context.Context, cc mcp.CallContext, raw json.RawMessage) (any, error) {
	a, err := decode[decideArgs](raw)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	r, ok := m.st.Access[a.RequestID]
	if !ok {
		m.mu.Unlock()
		return nil, mcp.Errorf("not_found", "заявка %s не найдена", a.RequestID)
	}
	if err := m.checkLead(cc.OnBehalfOf, r.EmployeeID); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if r.Status == "pending_approval" {
		switch a.Decision {
		case "approve":
			r.Status = "granted"
		case "reject":
			r.Status = "rejected"
		default:
			m.mu.Unlock()
			return nil, mcp.Errorf("invalid_arguments", "decision должен быть approve или reject")
		}
	}
	out := *r
	m.mu.Unlock()
	m.save()
	return out, nil
}

func (m *Mock) checklistComplete(_ context.Context, cc mcp.CallContext, raw json.RawMessage) (any, error) {
	a, err := decode[corp.ChecklistComplete](raw)
	if err != nil {
		return nil, err
	}
	emp, err := m.person(a.EmployeeID)
	if err != nil {
		return nil, err
	}
	if cc.OnBehalfOf != emp.ID && cc.OnBehalfOf != emp.MentorID && cc.OnBehalfOf != emp.LeadID {
		return nil, mcp.Errorf("forbidden", "отметить пункт может стажёр, его наставник или руководитель")
	}
	if a.ItemID == "" {
		return nil, mcp.Errorf("invalid_arguments", "item_id обязателен")
	}
	m.mu.Lock()
	items := m.st.Checklist[emp.ID]
	if items == nil {
		items = map[string]*corp.ChecklistItem{}
		m.st.Checklist[emp.ID] = items
	}
	it, ok := items[a.ItemID]
	if !ok || it.Status != "done" {
		it = &corp.ChecklistItem{EmployeeID: emp.ID, ItemID: a.ItemID, Status: "done", Evidence: a.Evidence, CompletedBy: cc.OnBehalfOf, CompletedAt: m.now().Format(time.RFC3339)}
		items[a.ItemID] = it
	}
	out := *it
	m.mu.Unlock()
	m.save()
	return out, nil
}

func (m *Mock) checklistGet(_ context.Context, cc mcp.CallContext, raw json.RawMessage) (any, error) {
	a, err := decode[struct {
		EmployeeID string `json:"employee_id"`
	}](raw)
	if err != nil {
		return nil, err
	}
	emp, err := m.person(a.EmployeeID)
	if err != nil {
		return nil, err
	}
	if cc.OnBehalfOf != emp.ID && cc.OnBehalfOf != emp.MentorID && cc.OnBehalfOf != emp.LeadID {
		return nil, mcp.Errorf("forbidden", "нет прав на чек-лист")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []corp.ChecklistItem{}
	for _, it := range m.st.Checklist[emp.ID] {
		out = append(out, *it)
	}
	slices.SortFunc(out, func(a, b corp.ChecklistItem) int {
		if a.ItemID < b.ItemID {
			return -1
		}
		return 1
	})
	return map[string]any{"items": out}, nil
}

// Handler returns the HTTP handler: /mcp, /healthz and token-protected /admin/*.
func (m *Mock) Handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/mcp", m.Server)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	admin := func(fn func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("POST /admin/reset", admin(func(w http.ResponseWriter, _ *http.Request) { m.Reset(); w.WriteHeader(http.StatusNoContent) }))
	mux.HandleFunc("POST /admin/fault", admin(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		m.SetFault(b.Enabled)
		w.WriteHeader(http.StatusNoContent)
	}))
	return mux
}
