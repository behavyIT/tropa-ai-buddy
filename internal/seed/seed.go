// Package seed fills a fresh database with synthetic demo data: two knowledge
// cards answered by a mentor in a previous (synthetic) cohort and analytics
// events of that cohort, so «Тропинки» are meaningful from the first minute.
package seed

import (
	"context"
	"fmt"
	"time"

	"tropa/internal/clock"
	"tropa/internal/domain"
	"tropa/internal/store"
)

// Cards are synthetic mentor answers from the previous cohort.
var Cards = []domain.KnowledgeCard{
	{ID: "seed-card-staging-token", TeamID: "ads-backend", AuthorID: "u-mentor-artem",
		Title: "Где взять тестовый токен для staging API?",
		Body:  "Тестовый токен для staging получают через avito-cli после авторизации — команда описана в README шаблона сервиса. Токен живёт сутки. Если получить не выходит — проверь, что у тебя уже есть доступ к k8s-staging. (Синтетический ответ наставника для прототипа.)"},
	{ID: "seed-card-lint", TeamID: "ads-backend", AuthorID: "u-mentor-artem",
		Title: "Почему пайплайн падает на шаге lint у нового сервиса?",
		Body:  "Чаще всего причина — устаревший конфиг линтера: возьми его из шаблона сервиса и прогони линтер локально перед пушем. Если упали тесты — открой лог конкретной джобы, а не общий статус пайплайна. (Синтетический ответ наставника для прототипа.)"},
}

// Run seeds data if the database has no knowledge cards yet. Idempotent.
func Run(ctx context.Context, st *store.Store) error {
	cards, err := st.ListCards(ctx)
	if err != nil {
		return err
	}
	if len(cards) > 0 {
		return nil
	}
	base := time.Date(2026, 10, 12, 12, 0, 0, 0, clock.MSK)
	for i, c := range Cards {
		c.CreatedAt = base.AddDate(0, 0, i*3)
		if err := st.InsertCard(ctx, c); err != nil {
			return err
		}
	}
	return seedEvents(ctx, st, base.AddDate(0, -2, 0))
}

// synthetic previous cohort: how many questions were asked while each step was current
var stepLoad = map[string]map[string]int{
	"ads-backend":     {"access-k8s": 9, "avito-cli": 7, "first-task": 6, "stash-keys": 4, "meet-team": 3, "access-standard": 2, "teach-sentry": 3},
	"search-frontend": {"access-standard": 2, "design-system": 5, "first-task": 6, "meet-team": 2},
}

var unanswered = [][]string{{"staging", "токен"}, {"vpn", "дом"}, {"кофемашин", "этаж"}, {"staging", "токен"}, {"парковк"}}

func seedEvents(ctx context.Context, st *store.Store, start time.Time) error {
	n := 0
	for team, steps := range stepLoad {
		for step, count := range steps {
			for i := 0; i < count; i++ {
				n++
				e := domain.Event{UserID: fmt.Sprintf("u-past-%s-%d", team, i%3+1), TeamID: team, Role: "intern", Name: "chat_turn",
					Props: map[string]any{"cohort": "synthetic", "step": step, "intent": "question"}, CreatedAt: start.Add(time.Duration(n) * time.Hour)}
				if err := st.InsertEvent(ctx, e); err != nil {
					return err
				}
			}
		}
		for i, hours := range []float64{30, 52, 18} {
			e := domain.Event{UserID: fmt.Sprintf("u-past-%s-%d", team, i+1), TeamID: team, Role: "intern", Name: "access_wait",
				Props: map[string]any{"cohort": "synthetic", "system": "k8s-staging", "hours": hours}, CreatedAt: start.Add(time.Duration(n+i) * time.Hour)}
			if err := st.InsertEvent(ctx, e); err != nil {
				return err
			}
		}
	}
	for i, terms := range unanswered {
		e := domain.Event{UserID: "u-past-ads-backend-1", TeamID: "ads-backend", Role: "intern", Name: "answer_shown",
			Props: map[string]any{"cohort": "synthetic", "answerable": false, "terms": terms}, CreatedAt: start.Add(time.Duration(i) * time.Minute)}
		if err := st.InsertEvent(ctx, e); err != nil {
			return err
		}
	}
	return nil
}
