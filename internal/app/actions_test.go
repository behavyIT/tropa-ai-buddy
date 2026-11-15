package app_test

import (
	"testing"

	"tropa/internal/domain"
)

// ---------- ABS: отгул ----------

func TestABS01_ExplainProcedure(t *testing.T) {
	h := newHarness(t)
	r := h.login("alex").chat("Как оформить отгул? Нужно ли отвлекать руководителя?")
	mustContain(t, r.Buddy.Text, "Мария Соколова", "сервисе отсутствий", "отвлекать")
	if len(r.Actions) != 0 {
		t.Fatal("explaining must not create actions")
	}
	if len(r.Buddy.Payload.Suggestions) < 2 {
		t.Fatal("expected date suggestions")
	}
}

func TestABS02_PlanWithApprover(t *testing.T) {
	h := newHarness(t)
	a := h.login("alex").chat("Оформи отгул на 11 ноября").action(t)
	if a.Type != domain.ActionAbsence || a.Status != domain.ActionPlanned || a.View.Approver != "Мария Соколова" {
		t.Fatalf("plan: %+v", a)
	}
}

func TestABS03_CalendarConflictWarning(t *testing.T) {
	h := newHarness(t)
	a := h.login("alex").chat("Хочу взять отгул в пятницу").action(t)
	if len(a.View.Warnings) == 0 {
		t.Fatal("expected sprint demo warning")
	}
	mustContain(t, a.View.Warnings[0], "Демо спринта")
	b := h.login("alex").chat("оформи отгул на завтра").action(t)
	found := false
	for _, w := range b.View.Warnings {
		found = found || contains(w, "меньше 2 рабочих дней")
	}
	if !found {
		t.Fatalf("expected short-notice warning: %+v", b.View.Warnings)
	}
}

func TestABS04_DateValidation(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	mustContain(t, alex.chat("оформи отгул на 2 ноября").Buddy.Text, "уже прошла")
	mustContain(t, alex.chat("оформи отгул с 16 по 20 ноября").Buddy.Text, "максимум на 3 дня")
	mustContain(t, alex.chat("оформи отгул на 14 ноября").Buddy.Text, "выходные")
	mustContain(t, alex.chat("хочу отгул, оформи").Buddy.Text, "На какую дату")
}

func TestABS05_ConfirmExecutesViaMCPAndSavesReceipt(t *testing.T) {
	h := newHarness(t)
	alex, artem := h.login("alex"), h.login("artem")
	a := alex.chat("Оформи отгул на 13 ноября").action(t)
	done := alex.confirm(a.ID)
	if done.Status != domain.ActionDone || done.ExternalStatus != domain.ExtPendingApproval || done.ExternalID == "" {
		t.Fatalf("receipt: %+v", done)
	}
	var list struct {
		Actions []domain.Action `json:"actions"`
	}
	alex.get("/api/actions", &list)
	if len(list.Actions) != 1 || list.Actions[0].ID != a.ID {
		t.Fatalf("receipts: %+v", list)
	}
	var n struct {
		Notifications []domain.Notification `json:"notifications"`
	}
	artem.get("/api/notifications", &n)
	if len(n.Notifications) != 1 || !contains(n.Notifications[0].Title, "оформил отгул") {
		t.Fatalf("mentor informed: %+v", n)
	}
	r := alex.chat("Что с моей заявкой на отгул?")
	if r.card("requests") == nil {
		t.Fatalf("status card: %+v", r.Buddy.Payload)
	}
	mustContain(t, r.Buddy.Text, "на согласовании")
}

func TestABS06_LeadApprovalUpdatesStatus(t *testing.T) {
	h := newHarness(t)
	alex, maria := h.login("alex"), h.login("maria")
	a := alex.chat("Оформи отгул на 13 ноября").action(t)
	alex.confirm(a.ID)
	var ap struct {
		Approvals []struct {
			Action    domain.Action `json:"action"`
			Kind      string        `json:"kind"`
			Requester string        `json:"requester"`
		} `json:"approvals"`
	}
	maria.get("/api/approvals", &ap)
	if len(ap.Approvals) != 1 || ap.Approvals[0].Kind != "Отгул" {
		t.Fatalf("approvals: %+v", ap)
	}
	var dec domain.Action
	if code := maria.post("/api/approvals/"+a.ID+"/decide", map[string]any{"approve": false, "comment": "демо"}, &dec); code != 200 || dec.ExternalStatus != domain.ExtRejected {
		t.Fatalf("decide: %d %+v", code, dec)
	}
	if code := maria.post("/api/approvals/"+a.ID+"/decide", map[string]any{"approve": true}, &map[string]any{}); code != 409 {
		t.Fatalf("second decision: %d", code)
	}
	var n struct {
		Unread int `json:"unread"`
	}
	alex.get("/api/notifications", &n)
	if n.Unread == 0 {
		t.Fatal("intern must be notified")
	}
	mustContain(t, alex.chat("статус моих заявок").Buddy.Text, "отклонена")
}

func TestABS07_FlagOffMeansExplainOnly(t *testing.T) {
	h := newHarness(t)
	maria := h.login("maria")
	if code := maria.do("PUT", "/api/flags/actions.absence", map[string]any{"enabled": false}, &map[string]any{}, true); code != 200 {
		t.Fatalf("flag: %d", code)
	}
	r := h.login("alex").chat("оформи отгул на 13 ноября")
	if len(r.Actions) != 0 {
		t.Fatal("no action when flag is off")
	}
	mustContain(t, r.Buddy.Text, "выключено")
}

// ---------- ACC: доступы ----------

func TestACC01_StandardBundleGrantedInstantly(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	a := alex.chat("Запроси стандартные доступы").action(t)
	if a.Type != domain.ActionAccess {
		t.Fatalf("type %s", a.Type)
	}
	if d := alex.confirm(a.ID); d.ExternalStatus != domain.ExtGranted {
		t.Fatalf("granted: %+v", d)
	}
}

func TestACC02_NonStandardNeedsLeadApproval(t *testing.T) {
	h := newHarness(t)
	alex, maria := h.login("alex"), h.login("maria")
	a := alex.chat("Нужен доступ к кубер staging").action(t)
	d := alex.confirm(a.ID)
	if d.ExternalStatus != domain.ExtPendingApproval {
		t.Fatalf("pending: %+v", d)
	}
	if alex.route("").status("access-k8s") != domain.TaskWaiting {
		t.Fatal("access-k8s must be waiting")
	}
	var dec domain.Action
	maria.post("/api/approvals/"+a.ID+"/decide", map[string]any{"approve": true}, &dec)
	if dec.ExternalStatus != domain.ExtGranted || alex.route("").status("access-k8s") != domain.TaskDone {
		t.Fatalf("after approval: %+v", dec)
	}
}

func TestACC03_ProductionForbiddenForInterns(t *testing.T) {
	h := newHarness(t)
	r := h.login("alex").chat("Дай доступ к проду, хочу посмотреть данные")
	if len(r.Actions) != 0 {
		t.Fatal("forbidden access must not be planned")
	}
	mustContain(t, r.Buddy.Text, "Не могу запросить", "стажёр")
	// a mentor may request it
	r = h.login("artem").chat("Запроси доступ к прод базе")
	if len(r.Actions) != 1 {
		t.Fatalf("mentor plan: %q", r.Buddy.Text)
	}
}

func TestACC04_SkipsExistingAndPending(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	alex.confirm(alex.chat("Запроси стандартные доступы").action(t).ID)
	r := alex.chat("Запроси стандартные доступы")
	if len(r.Actions) != 0 {
		t.Fatal("nothing to request")
	}
	mustContain(t, r.Buddy.Text, "Уже есть")
	alex.confirm(alex.chat("нужен доступ к kafka").action(t).ID)
	mustContain(t, alex.chat("нужен доступ к kafka").Buddy.Text, "Уже на согласовании")
	mustContain(t, alex.chat("как получить доступ к jira?").Buddy.Text, "стандартный набор")
}

// ---------- ACT: жизненный цикл действия ----------

func TestACT01_PlanHasNoSideEffects(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	alex.chat("Запроси стандартные доступы")
	if alex.route("").status("access-standard") == domain.TaskDone {
		t.Fatal("planning must not execute")
	}
}

func TestACT02_ConfirmIsExactlyOnce(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	a := alex.chat("Оформи отгул на 13 ноября").action(t)
	first := alex.confirm(a.ID)
	second := alex.confirm(a.ID)
	if first.ExternalID != second.ExternalID {
		t.Fatalf("double execution: %s vs %s", first.ExternalID, second.ExternalID)
	}
	if code := alex.post("/api/actions/"+a.ID+"/cancel", nil, &map[string]any{}); code != 409 {
		t.Fatalf("cancel done: %d", code)
	}
}

func TestACT03_OnlyOwnerCanConfirm(t *testing.T) {
	h := newHarness(t)
	a := h.login("alex").chat("Оформи отгул на 13 ноября").action(t)
	dana := h.login("dana")
	if code := dana.post("/api/actions/"+a.ID+"/confirm", nil, &map[string]any{}); code != 404 {
		t.Fatalf("foreign confirm: %d", code)
	}
	if code := dana.get("/api/actions/"+a.ID, &map[string]any{}); code != 404 {
		t.Fatalf("foreign read: %d", code)
	}
}

func TestACT04_ServiceOutageFailsGracefullyAndRetries(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	a := alex.chat("Оформи отгул на 13 ноября").action(t)
	if code := alex.post("/api/demo/corp-fault", map[string]bool{"enabled": true}, &map[string]any{}); code != 200 {
		t.Fatalf("fault: %d", code)
	}
	failed := alex.confirm(a.ID)
	if failed.Status != domain.ActionFailed || failed.Error == "" {
		t.Fatalf("expected failed with message: %+v", failed)
	}
	alex.post("/api/demo/corp-fault", map[string]bool{"enabled": false}, &map[string]any{})
	ok := alex.confirm(a.ID)
	if ok.Status != domain.ActionDone {
		t.Fatalf("retry: %+v", ok)
	}
}

func TestACT05_Cancel(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	a := alex.chat("Оформи отгул на 13 ноября").action(t)
	var c domain.Action
	if code := alex.post("/api/actions/"+a.ID+"/cancel", nil, &c); code != 200 || c.Status != domain.ActionCancelled {
		t.Fatalf("cancel: %d %+v", code, c)
	}
	if code := alex.post("/api/actions/"+a.ID+"/confirm", nil, &map[string]any{}); code != 409 {
		t.Fatalf("confirm cancelled: %d", code)
	}
}

func TestACT06_EditAbsencePlan(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	a := alex.chat("Оформи отгул на 13 ноября").action(t)
	var e domain.Action
	if code := alex.do("PATCH", "/api/actions/"+a.ID, map[string]string{"date_from": "2026-11-18", "date_to": "2026-11-19", "comment": "Семейные дела"}, &e, true); code != 200 {
		t.Fatalf("edit: %d", code)
	}
	if e.ID != a.ID || !contains(e.Title, "18 ноября") || len(e.View.Warnings) != 0 {
		t.Fatalf("edited: %+v", e)
	}
	if code := alex.do("PATCH", "/api/actions/"+a.ID, map[string]string{"date_from": "bad", "date_to": "2026-11-19"}, &map[string]any{}, true); code != 400 {
		t.Fatalf("bad date: %d", code)
	}
	alex.confirm(a.ID)
	if code := alex.do("PATCH", "/api/actions/"+a.ID, map[string]string{"date_from": "2026-11-18", "date_to": "2026-11-18"}, &map[string]any{}, true); code != 409 {
		t.Fatalf("edit after confirm: %d", code)
	}
}
