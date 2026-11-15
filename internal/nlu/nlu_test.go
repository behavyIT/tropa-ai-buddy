package nlu_test

import (
	"testing"
	"time"

	"tropa/content"
	"tropa/internal/catalog"
	"tropa/internal/clock"
	"tropa/internal/nlu"
)

var now = time.Date(2026, 11, 9, 10, 0, 0, 0, clock.MSK) // Monday

func parser(t *testing.T) *nlu.Parser {
	cat, err := catalog.Load(content.FS)
	if err != nil {
		t.Fatal(err)
	}
	return nlu.New(cat)
}

func d(s string) string { return s }

func TestNLU01_IntentDetection(t *testing.T) {
	p := parser(t)
	cases := map[string]string{
		"Привет!":                             nlu.IntentGreeting,
		"спасибо большое":                     nlu.IntentThanks,
		"что дальше?":                         nlu.IntentNextStep,
		"как оформить отгул":                  nlu.IntentAbsence,
		"нужен доступ к графане":              nlu.IntentAccess,
		"к кому обратиться по поводу отпуска": nlu.IntentWho,
		"спроси наставника про ревью":         nlu.IntentAskMentor,
		"хочу рассказать про grafana":         nlu.IntentTeachback,
		"что с моей заявкой на доступ":        nlu.IntentStatus,
		"я прочитал принципы разработки":      nlu.IntentCompleteTask,
		"какая зарплата у стажёров":           nlu.IntentSensitive,
		"как устроен деплой в staging":        nlu.IntentQuestion,
		"расскажи про продукт":                nlu.IntentQuestion,
	}
	for text, want := range cases {
		if got := p.Parse(text, now); got.Intent != want {
			t.Errorf("%q: got %s want %s", text, got.Intent, want)
		}
	}
	if u := p.Parse("запроси стандартный набор доступов", now); !u.Standard {
		t.Error("standard flag")
	}
	if u := p.Parse("как получить доступ к kafka?", now); u.Sub != nlu.SubHow || u.Systems[0] != "kafka-ui" {
		t.Errorf("access how: %+v", u)
	}
	if u := p.Parse("нужен прод доступ", now); len(u.Systems) != 1 || u.Systems[0] != "prod-db" {
		t.Errorf("prod: %+v", u.Systems)
	}
	if u := p.Parse("новый продукт и продвижение", now); len(u.Systems) != 0 {
		t.Errorf("prod false positive: %+v", u.Systems)
	}
}

func TestNLU02_RussianDates(t *testing.T) {
	cases := []struct{ text, from, to string }{
		{"на завтра", "2026-11-10", "2026-11-10"},
		{"послезавтра", "2026-11-11", "2026-11-11"},
		{"сегодня", "2026-11-09", "2026-11-09"},
		{"в пятницу", "2026-11-13", "2026-11-13"},
		{"в понедельник", "2026-11-16", "2026-11-16"},
		{"13 ноября", "2026-11-13", "2026-11-13"},
		{"с 13 по 14 ноября", "2026-11-13", "2026-11-14"},
		{"18-19 ноября", "2026-11-18", "2026-11-19"},
		{"13.11", "2026-11-13", "2026-11-13"},
		{"с 20.11.26 по 19.11.2026", "2026-11-19", "2026-11-20"},
		{"5 января", "2027-01-05", "2027-01-05"},
		{"1 мая", "2027-05-01", "2027-05-01"},
		{"3 марта", "2027-03-03", "2027-03-03"},
	}
	for _, c := range cases {
		f, to := nlu.ParseDates(c.text, now)
		if f == nil || f.Format("2006-01-02") != c.from || to.Format("2006-01-02") != c.to {
			t.Errorf("%q: got %v..%v want %s..%s", c.text, f, to, c.from, c.to)
		}
	}
	for _, s := range []string{"без даты", "31.02", "40 ноября"} {
		if f, _ := nlu.ParseDates(s, now); f != nil {
			t.Errorf("%q should have no date, got %v", s, f)
		}
	}
	if !nlu.IsSensitive("какой у него оклад") || nlu.IsSensitive("как дела") {
		t.Error("sensitive")
	}
	if p := parser(t); p.TeachTopic("про графану") != "grafana" || p.TeachTopic("про kafka") != "" {
		t.Error("teach topic")
	}
}
