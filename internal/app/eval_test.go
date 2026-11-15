package app_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestEVAL02_AnswerOrSayIDontKnow measures the offline knowledge-base behaviour:
// in-base questions must be answered from the right source, out-of-base ones must
// get an honest "не нашёл" with an offer to ask the mentor (never a made-up answer).
func TestEVAL02_AnswerOrSayIDontKnow(t *testing.T) {
	h := newHarness(t)
	alex := h.login("alex")
	inBase := []struct {
		q    string
		docs []string
	}{
		{"Во сколько у команды дейли?", []string{"ads-backend-team"}},
		{"Как проходит код-ревью?", []string{"code-review"}},
		{"Какого размера должен быть MR?", []string{"code-review"}},
		{"Что делать, если долго нет ревью?", []string{"code-review"}},
		{"Где взять первую задачу?", []string{"first-task"}},
		{"Что такое Sentry?", []string{"sentry"}},
		{"Зачем команде Grafana?", []string{"grafana"}},
		{"Как добавить SSH-ключ в stash?", []string{"ads-backend-tools"}},
		{"Что такое avito-cli?", []string{"ads-backend-tools"}},
		{"Как устроен CI/CD в команде?", []string{"ads-backend-tools"}},
		{"Через что смотреть поды на staging?", []string{"ads-backend-tools", "access-overview"}},
		{"Что такое экватор?", []string{"internship-review", "glossary"}},
		{"Что значит good-first-issue?", []string{"glossary", "first-task"}},
		{"Как работать с персональными данными пользователей?", []string{"data-protection"}},
		{"Где взять тестовый токен для staging?", []string{"card:seed-card-staging-token"}},
		{"Почему пайплайн падает на шаге lint?", []string{"card:seed-card-lint"}},
	}
	outOfBase := []string{
		"Как настроить принтер на пятом этаже?",
		"Где припарковать велосипед?",
		"Какой пароль от гостевого Wi-Fi?",
		"Когда будет новогодний корпоратив?",
		"Где купить мерч компании?",
		"Какую IDE лучше поставить для Go?",
		"До скольки работает столовая?",
		"Можно ли приходить в офис с собакой?",
		"Как называть ветки в монорепо фронтенда?",
		"Сколько стоит обучение английскому?",
	}
	var rows []string
	right := 0
	for _, c := range inBase {
		r := alex.chat(c.q)
		got := "—"
		if len(r.Buddy.Payload.Citations) > 0 {
			got = r.Buddy.Payload.Citations[0].DocID
		}
		ok := r.Buddy.Payload.Intent == "question" && slices.Contains(c.docs, got)
		if ok {
			right++
		}
		rows = append(rows, fmt.Sprintf("| %s | %s | `%s` | %s |", c.q, strings.Join(c.docs, " / "), got, mark(ok)))
	}
	honest := 0
	for _, q := range outOfBase {
		r := alex.chat(q)
		ok := r.Buddy.Payload.Intent == "question.no_answer" && len(r.Actions) == 1 && r.Actions[0].Type == "mentor.question"
		if ok {
			honest++
		}
		rows = append(rows, fmt.Sprintf("| %s | нет в базе → «не знаю» + вопрос наставнику | `%s` | %s |", q, r.Buddy.Payload.Intent, mark(ok)))
	}
	t.Logf("in-base answered from the right source: %d/%d; out-of-base honest no-answer: %d/%d", right, len(inBase), honest, len(outOfBase))
	if os.Getenv("EVAL_REPORT") == "1" {
		out := fmt.Sprintf("# База знаний: ответить по источнику или честно сказать «не знаю»\n\n"+
			"Офлайн-режим, стажёр команды «Объявления: Backend». Тест `internal/app/eval_test.go`.\n"+
			"Набор использовался и при настройке порогов — это регрессионный, а не слепой тест.\n\n"+
			"- **Вопросы из базы, ответ из верного источника: %d/%d**\n- **Вопросы вне базы, честное «не нашёл» + план вопроса наставнику: %d/%d** (выдуманных ответов: %d)\n\n"+
			"Порог «знаю/не знаю»: лучший фрагмент должен набрать BM25 ≥ 2,0 и покрыть ≥ 34%% значимых слов вопроса; фильтр прав применяется до ранжирования.\n\n"+
			"| Вопрос | Ожидаемый источник | Первый источник / интент | |\n|---|---|---|---|\n%s\n",
			right, len(inBase), honest, len(outOfBase), len(outOfBase)-honest, strings.Join(rows, "\n"))
		if err := os.WriteFile(filepath.Join("..", "..", "docs", "eval", "kb-answers.md"), []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if right < len(inBase)*85/100 || honest < len(outOfBase)*85/100 {
		t.Fatalf("quality below 85%%: answered %d/%d, honest %d/%d", right, len(inBase), honest, len(outOfBase))
	}
}

func mark(ok bool) string {
	if ok {
		return "✅"
	}
	return "❌"
}
