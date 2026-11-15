package corpmock_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tropa/content"
	"tropa/internal/catalog"
	"tropa/internal/clock"
	"tropa/internal/corp"
	"tropa/internal/corpmock"
	"tropa/internal/directory"
	"tropa/internal/mcp"
)

func setup(t *testing.T) (*corpmock.Mock, *corp.Client, *httptest.Server) {
	t.Helper()
	cat, err := catalog.Load(content.FS)
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 11, 9, 10, 0, 0, 0, clock.MSK) }
	m := corpmock.New(directory.New(), cat, "tok", now)
	m.StatePath = filepath.Join(t.TempDir(), "state.json")
	srv := httptest.NewServer(m.Handler("tok"))
	t.Cleanup(srv.Close)
	c := mcp.NewClient(srv.URL+"/mcp", "tok", 2*time.Second)
	c.Backoff = time.Millisecond
	return m, &corp.Client{MCP: c}, srv
}

func toolErr(t *testing.T, err error, code string) {
	t.Helper()
	var te *mcp.ToolError
	if !errors.As(err, &te) || te.Code != code {
		t.Fatalf("expected tool error %q, got %v", code, err)
	}
}

func TestMCP05_DelegatedIdentityEnforcedByService(t *testing.T) {
	_, c, _ := setup(t)
	ctx := context.Background()
	_, err := c.CreateAbsence(ctx, "u-intern-dana", corp.AbsenceCreate{EmployeeID: "u-intern-alex", Type: "day_off", DateFrom: "2026-11-13", DateTo: "2026-11-13", IdempotencyKey: "k"})
	toolErr(t, err, "forbidden")
	r, err := c.CreateAbsence(ctx, "u-intern-alex", corp.AbsenceCreate{EmployeeID: "u-intern-alex", Type: "day_off", DateFrom: "2026-11-13", DateTo: "2026-11-13", IdempotencyKey: "k"})
	if err != nil || r.Status != "pending_approval" || r.ApproverID != "u-lead-maria" {
		t.Fatalf("create: %+v %v", r, err)
	}
	again, _ := c.CreateAbsence(ctx, "u-intern-alex", corp.AbsenceCreate{EmployeeID: "u-intern-alex", Type: "day_off", DateFrom: "2026-11-13", DateTo: "2026-11-13", IdempotencyKey: "k"})
	if again.RequestID != r.RequestID {
		t.Fatal("idempotency key must dedupe")
	}
	_, err = c.DecideAbsence(ctx, "u-intern-alex", r.RequestID, true, "")
	toolErr(t, err, "forbidden")
	_, err = c.DecideAbsence(ctx, "u-lead-oleg", r.RequestID, true, "")
	toolErr(t, err, "forbidden")
	_, err = c.GetAbsence(ctx, "u-intern-dana", r.RequestID)
	toolErr(t, err, "forbidden")
	if got, err := c.GetAbsence(ctx, "u-hrbp-olga", r.RequestID); err != nil || got.RequestID != r.RequestID {
		t.Fatalf("hrbp read: %v", err)
	}
	d, err := c.DecideAbsence(ctx, "u-lead-maria", r.RequestID, true, "ok")
	if err != nil || d.Status != "approved" {
		t.Fatalf("decide: %+v %v", d, err)
	}
	if d2, _ := c.DecideAbsence(ctx, "u-lead-maria", r.RequestID, false, ""); d2.Status != "approved" {
		t.Fatal("decision is final")
	}
	_, err = c.GetAbsence(ctx, "u-intern-alex", "ABS-404")
	toolErr(t, err, "not_found")
	_, err = c.DecideAbsence(ctx, "u-lead-maria", "ABS-404", true, "")
	toolErr(t, err, "not_found")
}

func TestABS08_ServiceValidatesAbsence(t *testing.T) {
	_, c, _ := setup(t)
	ctx := context.Background()
	mk := func(from, to, typ string) error {
		_, err := c.CreateAbsence(ctx, "u-intern-alex", corp.AbsenceCreate{EmployeeID: "u-intern-alex", Type: typ, DateFrom: from, DateTo: to, IdempotencyKey: from + to + typ})
		return err
	}
	toolErr(t, mk("bad", "2026-11-13", "day_off"), "invalid_date")
	toolErr(t, mk("2026-11-13", "bad", "day_off"), "invalid_date")
	toolErr(t, mk("2026-11-13", "2026-11-13", "vacation"), "unsupported_type")
	toolErr(t, mk("2026-11-13", "2026-11-12", "day_off"), "invalid_range")
	toolErr(t, mk("2026-11-13", "2026-11-17", "day_off"), "too_long")
	toolErr(t, mk("2026-11-02", "2026-11-02", "day_off"), "in_past")
	_, err := c.CreateAbsence(ctx, "u-ghost", corp.AbsenceCreate{EmployeeID: "u-ghost", Type: "day_off", DateFrom: "2026-11-13", DateTo: "2026-11-13"})
	toolErr(t, err, "unknown_employee")
}

func TestACC06_ServiceEnforcesAccessRules(t *testing.T) {
	_, c, _ := setup(t)
	ctx := context.Background()
	_, err := c.CreateAccess(ctx, "u-intern-alex", corp.AccessCreate{EmployeeID: "u-intern-alex", System: "prod-db", IdempotencyKey: "1"})
	toolErr(t, err, "forbidden")
	_, err = c.CreateAccess(ctx, "u-intern-alex", corp.AccessCreate{EmployeeID: "u-intern-alex", System: "nope", IdempotencyKey: "2"})
	toolErr(t, err, "unknown_system")
	_, err = c.CreateAccess(ctx, "u-intern-dana", corp.AccessCreate{EmployeeID: "u-intern-alex", System: "jira", IdempotencyKey: "3"})
	toolErr(t, err, "forbidden")
	r, err := c.CreateAccess(ctx, "u-intern-alex", corp.AccessCreate{EmployeeID: "u-intern-alex", System: "kafka-ui", IdempotencyKey: "4"})
	if err != nil || r.Status != "pending_approval" {
		t.Fatalf("kafka: %+v %v", r, err)
	}
	if again, _ := c.CreateAccess(ctx, "u-intern-alex", corp.AccessCreate{EmployeeID: "u-intern-alex", System: "kafka-ui", IdempotencyKey: "4"}); again.RequestID != r.RequestID {
		t.Fatal("idempotent")
	}
	_, err = c.GetAccess(ctx, "u-intern-dana", r.RequestID)
	toolErr(t, err, "forbidden")
	_, err = c.GetAccess(ctx, "u-intern-alex", "ACC-404")
	toolErr(t, err, "not_found")
	_, err = c.DecideAccess(ctx, "u-intern-alex", r.RequestID, true, "")
	toolErr(t, err, "forbidden")
	_, err = c.DecideAccess(ctx, "u-lead-maria", "ACC-404", true, "")
	toolErr(t, err, "not_found")
	d, err := c.DecideAccess(ctx, "u-lead-maria", r.RequestID, false, "")
	if err != nil || d.Status != "rejected" {
		t.Fatalf("reject: %+v %v", d, err)
	}
	if mentorProd, err := c.CreateAccess(ctx, "u-mentor-artem", corp.AccessCreate{EmployeeID: "u-mentor-artem", System: "prod-db", IdempotencyKey: "5"}); err != nil || mentorProd.Status != "pending_approval" {
		t.Fatalf("mentor prod: %+v %v", mentorProd, err)
	}
}

func TestMCP07_CalendarAndChecklistTools(t *testing.T) {
	m, c, _ := setup(t)
	ctx := context.Background()
	ev, err := c.TeamEvents(ctx, "u-intern-alex", "ads-backend", "2026-11-13", "2026-11-13")
	if err != nil || len(ev.Events) != 1 {
		t.Fatalf("events: %+v %v", ev, err)
	}
	_, err = c.TeamEvents(ctx, "u-intern-dana", "ads-backend", "2026-11-13", "2026-11-13")
	toolErr(t, err, "forbidden")
	_, err = c.TeamEvents(ctx, "u-hrbp-olga", "nope", "2026-11-13", "2026-11-13")
	toolErr(t, err, "not_found")
	// a teammate's absence is visible as a count, never as a name
	a, _ := c.CreateAbsence(ctx, "u-mentor-artem", corp.AbsenceCreate{EmployeeID: "u-mentor-artem", Type: "day_off", DateFrom: "2026-11-18", DateTo: "2026-11-19", IdempotencyKey: "m"})
	ev, _ = c.TeamEvents(ctx, "u-intern-alex", "ads-backend", "2026-11-18", "2026-11-18")
	if ev.AbsentByDate["2026-11-18"] != 1 || a.RequestID == "" {
		t.Fatalf("absent count: %+v", ev)
	}
	it, err := c.CompleteChecklist(ctx, "u-mentor-artem", corp.ChecklistComplete{EmployeeID: "u-intern-alex", ItemID: "add-to-chats", IdempotencyKey: "x"})
	if err != nil || it.Status != "done" || it.CompletedBy != "u-mentor-artem" {
		t.Fatalf("checklist: %+v %v", it, err)
	}
	_, err = c.CompleteChecklist(ctx, "u-intern-dana", corp.ChecklistComplete{EmployeeID: "u-intern-alex", ItemID: "notes"})
	toolErr(t, err, "forbidden")
	_, err = c.CompleteChecklist(ctx, "u-intern-alex", corp.ChecklistComplete{EmployeeID: "u-intern-alex"})
	toolErr(t, err, "invalid_arguments")
	_, err = c.CompleteChecklist(ctx, "u-intern-alex", corp.ChecklistComplete{EmployeeID: "u-ghost", ItemID: "x"})
	toolErr(t, err, "unknown_employee")
	var list struct {
		Items []corp.ChecklistItem `json:"items"`
	}
	if err := c.MCP.Call(ctx, "u-intern-alex", corp.ToolChecklistGet, map[string]string{"employee_id": "u-intern-alex"}, &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("checklist get: %+v %v", list, err)
	}
	toolErr(t, c.MCP.Call(ctx, "u-intern-dana", corp.ToolChecklistGet, map[string]string{"employee_id": "u-intern-alex"}, nil), "forbidden")
	toolErr(t, c.MCP.Call(ctx, "u-intern-alex", corp.ToolChecklistGet, map[string]string{"employee_id": "u-ghost"}, nil), "unknown_employee")
	var catResp map[string]any
	if err := c.MCP.Call(ctx, "u-intern-alex", corp.ToolAccessCatalog, map[string]any{}, &catResp); err != nil || catResp["systems"] == nil {
		t.Fatalf("catalog: %v", err)
	}
	toolErr(t, c.MCP.Call(ctx, "u-intern-alex", corp.ToolAbsenceGet, map[string]any{"request_id": "x", "extra": 1}, nil), "invalid_arguments")
	// decision argument validation
	r, _ := c.CreateAccess(ctx, "u-intern-alex", corp.AccessCreate{EmployeeID: "u-intern-alex", System: "k8s-staging", IdempotencyKey: "k8"})
	toolErr(t, c.MCP.Call(ctx, "u-lead-maria", corp.ToolAccessDecide, map[string]any{"request_id": r.RequestID, "decision": "maybe"}, nil), "invalid_arguments")
	ab, _ := c.CreateAbsence(ctx, "u-intern-alex", corp.AbsenceCreate{EmployeeID: "u-intern-alex", Type: "day_off", DateFrom: "2026-11-25", DateTo: "2026-11-25", IdempotencyKey: "ab"})
	toolErr(t, c.MCP.Call(ctx, "u-lead-maria", corp.ToolAbsenceDecide, map[string]any{"request_id": ab.RequestID, "decision": "maybe"}, nil), "invalid_arguments")

	// persistence: a new mock instance restores state from the file
	m2 := corpmock.New(directory.New(), mustCat(t), "tok", nil)
	m2.StatePath = m.StatePath
	if err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	srv2 := httptest.NewServer(m2.Handler("tok"))
	defer srv2.Close()
	c2 := &corp.Client{MCP: mcp.NewClient(srv2.URL+"/mcp", "tok", time.Second)}
	if got, err := c2.GetAccess(ctx, "u-intern-alex", r.RequestID); err != nil || got.System != "k8s-staging" {
		t.Fatalf("restored: %+v %v", got, err)
	}
}

func mustCat(t *testing.T) *catalog.Catalog {
	cat, err := catalog.Load(content.FS)
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func TestMCP08_AdminEndpointsFaultAndLatency(t *testing.T) {
	m, c, srv := setup(t)
	ctx := context.Background()
	admin := corp.NewHTTPAdmin(srv.URL+"/mcp", "tok")
	if err := admin.SetFault(ctx, true); err != nil || !m.Fault() {
		t.Fatalf("fault on: %v", err)
	}
	c.MCP.Retries = 0
	if _, err := c.GetAbsence(ctx, "u-intern-alex", "x"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("outage must surface as 503: %v", err)
	}
	if err := admin.SetFault(ctx, false); err != nil || m.Fault() {
		t.Fatalf("fault off: %v", err)
	}
	if err := admin.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	bad := corp.NewHTTPAdmin(srv.URL+"/mcp", "wrong")
	if err := bad.Reset(ctx); err == nil {
		t.Fatal("admin must require token")
	}
	resp, _ := http.Post(srv.URL+"/admin/fault", "application/json", strings.NewReader("{"))
	if resp.StatusCode != 401 {
		t.Fatalf("unauth admin: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/admin/fault", strings.NewReader("{"))
	req.Header.Set("Authorization", "Bearer tok")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 400 {
		t.Fatalf("bad body: %d", resp.StatusCode)
	}
	m.Latency = 30 * time.Millisecond
	start := time.Now()
	if _, err := c.TeamEvents(ctx, "u-intern-alex", "ads-backend", "2026-11-13", "2026-11-13"); err != nil || time.Since(start) < 30*time.Millisecond {
		t.Fatalf("latency: %v", err)
	}
	short, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
	defer cancel()
	m.Latency = time.Second
	if _, err := c.TeamEvents(short, "u-intern-alex", "ads-backend", "2026-11-13", "2026-11-13"); err == nil {
		t.Fatal("expected timeout")
	}
	if err := (&corpmock.Mock{StatePath: filepath.Join(t.TempDir(), "missing.json")}).Load(); err != nil {
		t.Fatal(err)
	}
}
