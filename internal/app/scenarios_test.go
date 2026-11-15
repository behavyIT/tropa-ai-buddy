package app_test

import (
	"strings"
	"testing"

	"tropa/internal/domain"
)

// ---------- ROUTE: этапы, статусы и следующие шаги ----------

func TestROUTE01_StagesStatusesAndProgress(t *testing.T) {
	h := newHarness(t)
	v := h.login("alex").route("")
	if v.Day != 1 || v.Total != 22 || v.Done != 0 || len(v.Stages) != 4 {
		t.Fatalf("unexpected route header: day=%d total=%d done=%d stages=%d", v.Day, v.Total, v.Done, len(v.Stages))
	}
	if v.status("meet-mentor") != domain.TaskTodo {
		t.Fatalf("meet-mentor should be todo, got %s", v.status("meet-mentor"))
	}
}

func TestROUTE02_DependenciesBlockTasks(t *testing.T) {
	h := newHarness(t)
	v := h.login("alex").route("")
	if v.status("avito-cli") != domain.TaskBlocked || v.status("first-task") != domain.TaskBlocked {
		t.Fatalf("dependent tasks must be blocked: avito-cli=%s first-task=%s", v.status("avito-cli"), v.status("first-task"))
	}
}

func TestROUTE03_NextBestStepWithExplanation(t *testing.T) {
	h := newHarness(t)
	v := h.login("alex").route("")
	if len(v.Next) != 3 || v.Next[0].TaskID != "meet-mentor" {
		t.Fatalf("next steps: %+v", v.Next)
	}
	mustContain(t, v.Next[0].Why, "Срок — сегодня")
	mustContain(t, v.Next[1].Why, "откроет")
}

func TestROUTE04_WaitingAndWhileWaiting(t *testing.T) {
	h := newHarness(t)
	v := h.login("alex").route("")
	if len(v.Waiting) == 0 || len(v.WhileWaiting) == 0 {
		t.Fatalf("expected waiting and while-waiting suggestions: %+v / %+v", v.Waiting, v.WhileWaiting)
	}
	mustContain(t, v.Waiting[0].Why, "Артём Ким")
}

func TestROUTE05_AccessTasksFollowAccessService(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	a := alex.chat("Запроси стандартные доступы").action(t)
	alex.confirm(a.ID)
	v := alex.route("")
	if v.status("access-standard") != domain.TaskDone || v.status("avito-cli") != domain.TaskTodo {
		t.Fatalf("access-standard=%s avito-cli=%s", v.status("access-standard"), v.status("avito-cli"))
	}
}

func TestROUTE06_MentorCompletesMentorOwnedTask(t *testing.T) {
	h := newHarness(t)
	alex, artem := h.login("alex"), h.login("artem")
	mv := artem.route("u-intern-alex")
	can := false
	for _, s := range mv.Stages {
		for _, tk := range s.Tasks {
			if tk.ID == "add-to-chats" {
				can = tk.CanComplete
			}
		}
	}
	if !can {
		t.Fatal("mentor must be able to complete add-to-chats")
	}
	var a domain.Action
	if code := artem.post("/api/route/tasks/add-to-chats/complete?user=u-intern-alex", nil, &a); code != 200 || a.ExternalStatus != domain.ExtCompleted {
		t.Fatalf("complete: %d %+v", code, a)
	}
	if alex.route("").status("meet-team") != domain.TaskTodo {
		t.Fatal("meet-team should unblock after mentor completed add-to-chats")
	}
	// intern cannot complete a mentor-owned task
	if code := alex.post("/api/route/tasks/goals-meeting/complete", nil, &map[string]any{}); code != 403 {
		t.Fatalf("intern completing lead task: %d", code)
	}
}

func TestROUTE07_NudgeMentorWithoutAwkwardness(t *testing.T) {
	h := newHarness(t)
	alex, artem := h.login("alex"), h.login("artem")
	if code := alex.post("/api/route/tasks/add-to-chats/nudge", nil, &map[string]any{}); code != 200 {
		t.Fatalf("nudge: %d", code)
	}
	if code := alex.post("/api/route/tasks/add-to-chats/nudge", nil, &map[string]any{}); code != 429 {
		t.Fatalf("second nudge should be rate limited: %d", code)
	}
	var n struct {
		Notifications []domain.Notification `json:"notifications"`
		Unread        int                   `json:"unread"`
	}
	artem.get("/api/notifications", &n)
	if n.Unread != 1 || n.Notifications[0].Kind != "nudge" {
		t.Fatalf("mentor notifications: %+v", n)
	}
	if code := alex.post("/api/route/tasks/meet-team/nudge", nil, &map[string]any{}); code != 404 {
		t.Fatalf("nudging own task: %d", code)
	}
}

func TestROUTE08_StartTaskAndCompleteManual(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	var v routeView
	if code := alex.post("/api/route/tasks/notes/start", nil, &v); code != 200 || v.status("notes") != domain.TaskInProgress {
		t.Fatalf("start: %d %s", code, v.status("notes"))
	}
	var a domain.Action
	if code := alex.post("/api/route/tasks/notes/complete", nil, &a); code != 200 || a.Status != domain.ActionDone {
		t.Fatalf("complete: %d %+v", code, a)
	}
	if alex.route("").status("notes") != domain.TaskDone {
		t.Fatal("notes should be done")
	}
	if code := alex.post("/api/route/tasks/nope/start", nil, &map[string]any{}); code != 404 {
		t.Fatalf("unknown task: %d", code)
	}
}

// ---------- QA: ответы с учётом контекста и прав ----------

func TestQA01_GroundedAnswerWithCitations(t *testing.T) {
	h := newHarness(t)
	r := h.login("alex").chat("Во сколько у команды дейли?")
	mustContain(t, r.Buddy.Text, "11:00", "[1]")
	if len(r.Buddy.Payload.Citations) == 0 || r.Buddy.Payload.Citations[0].DocID != "ads-backend-team" {
		t.Fatalf("citations: %+v", r.Buddy.Payload.Citations)
	}
}

func TestQA02_RestrictedDocNeverReachesIntern(t *testing.T) {
	h := newHarness(t)
	r := h.login("alex").chat("Какие критерии ревью компетенций стажёров и шкала оценки?")
	for _, c := range r.Buddy.Payload.Citations {
		if c.DocID == "review-criteria-mentors" {
			t.Fatal("restricted document leaked to intern")
		}
	}
	if strings.Contains(r.Buddy.Text, "четырём осям") {
		t.Fatal("restricted text leaked")
	}
	// mentor sees it
	m := h.login("artem").chat("Какие критерии ревью компетенций стажёров и шкала оценки?")
	found := false
	for _, c := range m.Buddy.Payload.Citations {
		found = found || c.DocID == "review-criteria-mentors"
	}
	if !found {
		t.Fatalf("mentor should get restricted doc: %+v", m.Buddy.Payload.Citations)
	}
}

func TestQA03_TeamContextIsolation(t *testing.T) {
	h := newHarness(t)
	r := h.login("dana").chat("Во сколько у команды дейли?")
	mustContain(t, r.Buddy.Text, "10:30")
	for _, c := range r.Buddy.Payload.Citations {
		if c.DocID == "ads-backend-team" {
			t.Fatal("other team's doc leaked")
		}
	}
}

func TestQA04_NoAnswerOffersEscalation(t *testing.T) {
	h := newHarness(t)
	r := h.login("alex").chat("Как настроить принтер в переговорке на пятом этаже?")
	if r.Buddy.Payload.Intent != "question.no_answer" {
		t.Fatalf("intent %s", r.Buddy.Payload.Intent)
	}
	if a := r.action(t); a.Type != domain.ActionQuestion || a.Status != domain.ActionPlanned {
		t.Fatalf("expected planned mentor question: %+v", a)
	}
}

func TestQA05_MentorAnswerCardReused(t *testing.T) {
	h := newHarness(t)
	r := h.login("alex").chat("Где взять тестовый токен для staging?")
	if r.Buddy.Payload.Citations[0].Kind != "mentor_answer" {
		t.Fatalf("expected mentor answer citation: %+v", r.Buddy.Payload.Citations)
	}
	mustContain(t, r.Buddy.Text, "уже отвечал")
}

func TestQA06_FeedbackOnAnswers(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	r := alex.chat("Как проходит код-ревью?")
	var m domain.Message
	if code := alex.post("/api/chat/messages/"+r.Buddy.ID+"/feedback", map[string]bool{"helpful": true}, &m); code != 200 || m.Feedback != 1 {
		t.Fatalf("feedback: %d %+v", code, m)
	}
	if code := alex.post("/api/chat/messages/"+r.User.ID+"/feedback", map[string]bool{"helpful": false}, &map[string]any{}); code != 404 {
		t.Fatalf("feedback on own message must 404: %d", code)
	}
	var hist struct {
		Messages []domain.Message `json:"messages"`
	}
	alex.get("/api/chat", &hist)
	if len(hist.Messages) != 2 {
		t.Fatalf("history: %d", len(hist.Messages))
	}
}

func TestQA07_TestInstructionsLabelled(t *testing.T) {
	h := newHarness(t)
	r := h.login("alex").chat("Как оформить отгул?")
	if r.Buddy.Payload.Citations[0].Kind != "test_instruction" {
		t.Fatalf("citation kind: %+v", r.Buddy.Payload.Citations)
	}
	mustContain(t, r.Buddy.Text, "тестовому правилу")
}

func TestQA08_WhoToAsk(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	r := alex.chat("К кому обратиться, если упал пайплайн?")
	if c := r.card("person"); c == nil || c.Person.ID != "u-mentor-artem" {
		t.Fatalf("person card: %+v", r.Buddy.Payload.Cards)
	}
	r = alex.chat("Кто мой наставник?")
	mustContain(t, r.Buddy.Text, "Артём Ким")
	r = alex.chat("Кто руководитель команды?")
	mustContain(t, r.Buddy.Text, "Мария Соколова")
	r = alex.chat("К кому обратиться насчёт парковки велосипеда?")
	mustContain(t, r.Buddy.Text, "Начни с наставника")
}

// ---------- SMALLTALK / NEXT STEP in chat ----------

func TestCHAT01_GreetingAndNextStepCards(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	r := alex.chat("Привет")
	if r.card("steps") == nil || len(r.Buddy.Payload.Suggestions) == 0 {
		t.Fatalf("greeting should carry steps & suggestions: %+v", r.Buddy.Payload)
	}
	r = alex.chat("Что мне делать дальше?")
	mustContain(t, r.Buddy.Text, "День 1")
	if r.card("steps") == nil {
		t.Fatal("next step card missing")
	}
	r = alex.chat("Спасибо!")
	if r.Buddy.Payload.Intent != "thanks" {
		t.Fatalf("intent %s", r.Buddy.Payload.Intent)
	}
	m := h.login("maria").chat("Привет")
	mustContain(t, m.Buddy.Text, "AI-бадди стажёров")
	mm := h.login("maria").chat("что дальше по плану")
	mustContain(t, mm.Buddy.Text, "только у стажёров")
}

func TestCHAT02_InputValidation(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	var r reply
	if code := alex.post("/api/chat", map[string]string{"text": "   "}, &r); code != 400 {
		t.Fatalf("empty: %d", code)
	}
	if code := alex.post("/api/chat", map[string]string{"text": strings.Repeat("я", 2001)}, &r); code != 400 {
		t.Fatalf("too long: %d", code)
	}
	if code := alex.post("/api/chat", map[string]any{"text": "x", "evil": 1}, &r); code != 400 {
		t.Fatalf("unknown field: %d", code)
	}
}
