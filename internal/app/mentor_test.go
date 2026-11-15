package app_test

import (
	"testing"

	"tropa/internal/domain"
)

type inbox struct {
	Questions []struct {
		domain.Question
		AskCount   int      `json:"ask_count"`
		AskerNames []string `json:"asker_names"`
	} `json:"questions"`
}

// ---------- ESC: «Спросить без неловкости» ----------

func TestESC01_PlanQuestionWithPrivacyNote(t *testing.T) {
	h := newHarness(t)
	r := h.login("alex").chat("Спроси наставника: можно ли брать задачи из соседнего сервиса?")
	a := r.action(t)
	if a.Type != domain.ActionQuestion {
		t.Fatalf("type %s", a.Type)
	}
	mustContain(t, a.View.Permission, "приватной")
	mustContain(t, r.Buddy.Text, "не нашу переписку")
}

func TestESC02_SimilarQuestionsAreMerged(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	alex.confirm(alex.chat("Спроси наставника: как попасть на внутренний митап по Go?").action(t).ID)
	second := alex.confirm(alex.chat("Спроси наставника: как попасть на внутренний митап по Go").action(t).ID)
	var res struct {
		Merged bool `json:"merged"`
	}
	_ = jsonUnmarshal(second.Result, &res)
	if !res.Merged {
		t.Fatalf("second similar question should merge: %s", second.Result)
	}
	var ib inbox
	h.login("artem").get("/api/mentor/questions", &ib)
	if len(ib.Questions) != 1 {
		t.Fatalf("questions: %d", len(ib.Questions))
	}
}

func TestESC03_UrgentNotifiesImmediately(t *testing.T) {
	h := newHarness(t)
	alex, artem := h.login("alex"), h.login("artem")
	alex.confirm(alex.chat("Спроси наставника: срочно, не могу работать — не собирается сервис").action(t).ID)
	var n struct {
		Notifications []domain.Notification `json:"notifications"`
	}
	artem.get("/api/notifications", &n)
	if len(n.Notifications) == 0 || !contains(n.Notifications[0].Title, "Срочный") {
		t.Fatalf("urgent notification: %+v", n)
	}
}

func TestESC04_AnswerReachesAskerChat(t *testing.T) {
	h := newHarness(t)
	alex, artem := h.login("alex"), h.login("artem")
	act := alex.confirm(alex.chat("Спроси наставника: где найти записи прошлых демо?").action(t).ID)
	var q domain.Question
	if code := artem.post("/api/mentor/questions/"+act.ExternalID+"/answer", map[string]any{"answer": "В Confluence, раздел Демо.", "publish": false}, &q); code != 200 || q.Status != domain.ExtAnswered {
		t.Fatalf("answer: %d %+v", code, q)
	}
	var hist struct {
		Messages []domain.Message `json:"messages"`
	}
	alex.get("/api/chat", &hist)
	last := hist.Messages[len(hist.Messages)-1]
	mustContain(t, last.Text, "Артём Ким ответил", "Confluence")
	var acts struct {
		Actions []domain.Action `json:"actions"`
	}
	alex.get("/api/actions", &acts)
	if acts.Actions[0].ExternalStatus != domain.ExtAnswered {
		t.Fatalf("receipt status: %+v", acts.Actions[0])
	}
	if code := artem.post("/api/mentor/questions/"+act.ExternalID+"/answer", map[string]any{"answer": "ещё раз"}, &map[string]any{}); code != 409 {
		t.Fatalf("double answer: %d", code)
	}
}

func TestESC05_PublishedAnswerBecomesKnowledge(t *testing.T) {
	h := newHarness(t)
	alex, artem, dana := h.login("alex"), h.login("artem"), h.login("dana")
	act := alex.confirm(alex.chat("Спроси наставника: как заказать пропуск для гостя на хакатон?").action(t).ID)
	artem.post("/api/mentor/questions/"+act.ExternalID+"/answer", map[string]any{"answer": "Пропуск для гостя заказывают через ресепшен за день до визита.", "publish": true}, &map[string]any{})
	r := alex.chat("Как заказать пропуск для гостя?")
	if r.Buddy.Payload.Citations[0].Kind != "mentor_answer" {
		t.Fatalf("card not used: %+v", r.Buddy.Payload.Citations)
	}
	// knowledge cards are team-scoped
	rd := dana.chat("Как заказать пропуск для гостя?")
	for _, c := range rd.Buddy.Payload.Citations {
		if c.Kind == "mentor_answer" {
			t.Fatal("card of another team leaked")
		}
	}
}

func TestESC06_OnlyAddressedMentorAnswers(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	act := alex.confirm(alex.chat("Спроси наставника: можно ли работать из другого города?").action(t).ID)
	if code := h.login("ilya").post("/api/mentor/questions/"+act.ExternalID+"/answer", map[string]any{"answer": "да"}, &map[string]any{}); code != 403 {
		t.Fatalf("foreign mentor: %d", code)
	}
	if code := alex.get("/api/mentor/questions", &map[string]any{}); code != 403 {
		t.Fatalf("intern inbox: %d", code)
	}
	var ib inbox
	h.login("maria").get("/api/mentor/questions", &ib)
	if len(ib.Questions) != 1 {
		t.Fatalf("lead sees team questions: %d", len(ib.Questions))
	}
	if code := h.login("artem").post("/api/mentor/questions/"+act.ExternalID+"/answer", map[string]any{"answer": " "}, &map[string]any{}); code != 400 {
		t.Fatalf("empty answer: %d", code)
	}
}

func TestESC07_AskMentorMultiTurn(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	r := alex.chat("спроси наставника")
	mustContain(t, r.Buddy.Text, "Сформулируй вопрос")
	r = alex.chat("Как часто у нас релизы в прод?")
	if r.action(t).Type != domain.ActionQuestion {
		t.Fatal("expected question plan")
	}
	alex.chat("спроси у наставника")
	mustContain(t, alex.chat("отмена").Buddy.Text, "не отправляю")
}

// ---------- TB: «Расскажи мне» ----------

func TestTB01_StartWithRubricPrompt(t *testing.T) {
	h := newHarness(t)
	r := h.login("alex").chat("Хочу рассказать про Sentry")
	mustContain(t, r.Buddy.Text, "Расскажи о Sentry", "своими словами")
}

func TestTB02_GradeShowsCoveredAndMissing(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	alex.chat("Хочу рассказать про Grafana")
	r := alex.chat("Grafana — это дашборды из панелей, на которых видно графики метрик сервиса, я буду туда смотреть.")
	tb := r.card("teachback")
	if tb == nil || tb.Teachback.Passed || len(tb.Teachback.Missing) == 0 || tb.Teachback.Missing[0].Hint == "" {
		t.Fatalf("teachback card: %+v", r.Buddy.Payload.Cards)
	}
}

func TestTB03_RetryAccumulatesAndPasses(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	alex.chat("Хочу рассказать про Grafana")
	alex.chat("Grafana — это дашборды из панелей, на которых видно графики метрик сервиса.")
	r := alex.chat("Данные берутся из источников вроде Prometheus через запросы PromQL, а по порогам настраивают алерты.")
	if tb := r.card("teachback"); tb == nil || !tb.Teachback.Passed {
		t.Fatalf("should pass after retry: %+v", r.Buddy.Payload.Cards)
	}
}

func TestTB04_PassPlansChecklistCompletion(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	alex.chat("Хочу рассказать про Sentry")
	r := alex.chat("Sentry собирает ошибки и исключения из сервиса, показывает стектрейс и окружение. Одинаковые события группируются в issue, а алерты приходят в канал команды.")
	a := r.action(t)
	if a.Type != domain.ActionChecklist {
		t.Fatalf("type %s", a.Type)
	}
	alex.confirm(a.ID)
	if alex.route("").status("teach-sentry") != domain.TaskDone {
		t.Fatal("teach-sentry should be done")
	}
	mustContain(t, alex.chat("хочу рассказать про sentry").Buddy.Text, "Давай")
}

func TestTB05_CancelAndShortAnswer(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	alex.chat("проверь меня по sentry")
	mustContain(t, alex.chat("ну это ошибки").Buddy.Text, "подробнее")
	mustContain(t, alex.chat("отмена").Buddy.Text, "вернёмся")
	r := alex.chat("хочу рассказать")
	if r.Buddy.Payload.Intent != "teachback" {
		t.Fatalf("intent %s", r.Buddy.Payload.Intent)
	}
	mustContain(t, r.Buddy.Text, "Расскажи о Sentry")
}

func TestTB06_CompleteTaskByChat(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	r := alex.chat("Я завёл конспект встреч с наставником, отметь")
	a := r.action(t)
	if a.Type != domain.ActionChecklist {
		t.Fatalf("type %s", a.Type)
	}
	mustContain(t, alex.chat("готово: рассказал про grafana").Buddy.Text, "Расскажи мне")
	mustContain(t, alex.chat("отметь, что я сделал стандартные доступы").Buddy.Text, "закроется сам")
	mustContain(t, alex.chat("готово, сделал что-то непонятное").Buddy.Text, "Не понял")
}
