package nlu_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"tropa/internal/nlu"
)

// goldenIntents is a labelled set of intern phrasings (incl. colloquial ones).
// It was also used while tuning the parser, so it is a regression set, not a blind test.
var goldenIntents = []struct{ text, intent string }{
	{"Привет", nlu.IntentGreeting}, {"Добрый день!", nlu.IntentGreeting}, {"здравствуйте", nlu.IntentGreeting},
	{"спасибо", nlu.IntentThanks}, {"Спасибо, помогло", nlu.IntentThanks},
	{"Что мне делать дальше?", nlu.IntentNextStep}, {"с чего начать?", nlu.IntentNextStep},
	{"какие у меня задачи на сегодня", nlu.IntentNextStep}, {"покажи мой прогресс", nlu.IntentNextStep},
	{"что осталось сделать по плану", nlu.IntentNextStep},
	{"Как оформить отгул?", nlu.IntentAbsence}, {"хочу взять выходной в пятницу", nlu.IntentAbsence},
	{"мне нужно отпроситься завтра", nlu.IntentAbsence}, {"не смогу прийти 13 ноября", nlu.IntentAbsence},
	{"оформи отгул с 18 по 19 ноября", nlu.IntentAbsence}, {"нужно ли предупреждать руководителя про отгул", nlu.IntentAbsence},
	{"нужен доступ к кубернетесу", nlu.IntentAccess}, {"запроси доступы", nlu.IntentAccess},
	{"как получить доступ к графане?", nlu.IntentAccess}, {"дай права на gitlab", nlu.IntentAccess},
	{"хочу доступ к kafka", nlu.IntentAccess},
	{"к кому обратиться с вопросом по отпуску", nlu.IntentWho}, {"кто мой наставник?", nlu.IntentWho},
	{"кому писать если упал пайплайн", nlu.IntentWho}, {"кто отвечает за доступы", nlu.IntentWho},
	{"спроси наставника, где лежат шаблоны", nlu.IntentAskMentor}, {"передай наставнику вопрос про ревью", nlu.IntentAskMentor},
	{"задать вопрос наставнику", nlu.IntentAskMentor},
	{"хочу рассказать про sentry", nlu.IntentTeachback}, {"проверь меня по графане", nlu.IntentTeachback},
	{"давай расскажу про Sentry", nlu.IntentTeachback},
	{"что с моей заявкой на отгул?", nlu.IntentStatus}, {"статус моих заявок", nlu.IntentStatus},
	{"одобрили мой доступ?", nlu.IntentStatus},
	{"я настроил avito-cli", nlu.IntentCompleteTask}, {"готово, прочитал принципы разработки", nlu.IntentCompleteTask},
	{"отметь знакомство с командой", nlu.IntentCompleteTask},
	{"сколько получает мой наставник", nlu.IntentSensitive}, {"как оформить больничный", nlu.IntentSensitive},
	{"какая премия у стажёров", nlu.IntentSensitive}, {"дай паспортные данные Марии", nlu.IntentSensitive},
	{"как у нас проходит код-ревью?", nlu.IntentQuestion}, {"что такое staging?", nlu.IntentQuestion},
	{"во сколько дейли", nlu.IntentQuestion}, {"где посмотреть логи сервиса", nlu.IntentQuestion},
	{"расскажи про Sentry", nlu.IntentQuestion}, {"как работает CI/CD в команде", nlu.IntentQuestion},
	{"что делать, если долго нет ревью?", nlu.IntentQuestion}, {"что делать, если упали тесты", nlu.IntentQuestion},
}

// TestEVAL01_IntentGoldenSet measures offline intent accuracy and guards against regressions.
func TestEVAL01_IntentGoldenSet(t *testing.T) {
	p := parser(t)
	ok := 0
	perIntent := map[string][2]int{}
	var misses []string
	for _, g := range goldenIntents {
		got := p.Parse(g.text, now).Intent
		c := perIntent[g.intent]
		c[1]++
		if got == g.intent {
			ok++
			c[0]++
		} else {
			misses = append(misses, fmt.Sprintf("«%s»: ожидали %s, получили %s", g.text, g.intent, got))
		}
		perIntent[g.intent] = c
	}
	acc := float64(ok) / float64(len(goldenIntents))
	t.Logf("intent accuracy %d/%d = %.1f%%", ok, len(goldenIntents), 100*acc)
	for _, m := range misses {
		t.Log("miss:", m)
	}
	if os.Getenv("EVAL_REPORT") == "1" {
		writeReport(t, ok, perIntent, misses)
	}
	if acc < 0.9 {
		t.Fatalf("intent accuracy %.2f below 0.90", acc)
	}
}

func writeReport(t *testing.T, ok int, per map[string][2]int, misses []string) {
	var keys []string
	for k := range per {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "# Офлайн-NLU: точность распознавания интентов\n\nНабор: %d размеченных формулировок стажёра (в т.ч. разговорных), `internal/nlu/eval_test.go`.\n", len(goldenIntents))
	fmt.Fprintf(&b, "Набор использовался и при настройке парсера — это регрессионный, а не слепой тест.\n\n**Точность: %d/%d = %.1f%%**\n\n| Интент | Верно |\n|---|---|\n", ok, len(goldenIntents), 100*float64(ok)/float64(len(goldenIntents)))
	for _, k := range keys {
		fmt.Fprintf(&b, "| `%s` | %d/%d |\n", k, per[k][0], per[k][1])
	}
	b.WriteString("\n## Ошибки\n")
	if len(misses) == 0 {
		b.WriteString("Нет.\n")
	}
	for _, m := range misses {
		b.WriteString("- " + m + "\n")
	}
	b.WriteString("\nНераспознанное в офлайн-режиме уходит в поиск по базе знаний; в LLM-режиме интент уточняет модель по JSON-схеме.\n")
	if err := os.WriteFile(filepath.Join("..", "..", "docs", "eval", "nlu-intents.md"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
