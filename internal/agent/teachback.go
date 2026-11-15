package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"tropa/internal/catalog"
	"tropa/internal/domain"
	"tropa/internal/flags"
	"tropa/internal/llm"
	"tropa/internal/textutil"
)

// "Расскажи мне" — the checklist literally asks interns to "Расскажи о Sentry /
// Grafana". The buddy becomes the listener (Feynman technique): grades the
// explanation against a rubric, points to gaps and, on success, offers to mark
// the checklist item done — the mentor no longer has to listen to every one.

func (a *Agent) teachTask(ctx context.Context, t *turn, topic string) (catalog.Task, bool) {
	r, err := a.route.RouteFor(t.u)
	if err != nil {
		return catalog.Task{}, false
	}
	states, _ := a.st.TaskStates(ctx, t.u.ID)
	for _, task := range r.AllTasks() {
		if task.Kind == "teachback" && (topic == "" || task.Topic == topic) && states[task.ID].Status != domain.TaskDone {
			return task, true
		}
	}
	return catalog.Task{}, false
}

func (a *Agent) startTeachback(ctx context.Context, t *turn, topic string) {
	if !a.flags.Enabled(ctx, flags.Teachback, t.u.TeamID) {
		t.say("Режим «Расскажи мне» в твоей команде сейчас выключен — такие пункты принимает наставник на 1:1.")
		return
	}
	task, hasTask := a.teachTask(ctx, t, topic)
	if topic == "" && hasTask {
		topic = task.Topic
	}
	rub, ok := a.cat.Rubrics[topic]
	if !ok {
		var names []string
		for _, r := range a.cat.Rubrics {
			names = append(names, r.Title)
		}
		slices.Sort(names)
		t.say("Про что расскажешь? Сейчас я умею принимать: " + strings.Join(names, ", ") + ".")
		for _, n := range names {
			t.suggest("Хочу рассказать про " + n)
		}
		return
	}
	st := map[string]string{"await": "teachback", "topic": topic}
	if hasTask && task.Topic == topic {
		st["task"] = task.ID
	}
	t.newSt = st
	t.say(fmt.Sprintf("Давай! Пункт чек-листа «Расскажи о %s» — я послушаю. %s\n\nЕсли захочешь выйти — напиши «отмена».", rub.Title, rub.Prompt))
	t.suggest("Отмена")
	a.notif.Track(ctx, t.u, "teachback_started", map[string]any{"topic": topic})
}

func (a *Agent) gradeTeachback(ctx context.Context, t *turn) {
	topic := t.state["topic"]
	rub, ok := a.cat.Rubrics[topic]
	if !ok {
		t.newSt = map[string]string{}
		a.handleQuestion(ctx, t)
		return
	}
	t.p.Intent = "teachback.grade"
	explanation := strings.TrimSpace(t.state["acc"] + "\n" + t.text)
	if len(strings.Fields(t.text)) < 6 && t.state["acc"] == "" {
		t.newSt = t.state
		t.say("Расскажи чуть подробнее — хотя бы 3–4 предложения своими словами. Не страшно ошибиться: я подскажу, что упущено.")
		t.suggest("Отмена")
		return
	}
	covered := a.grade(ctx, t, rub, explanation)
	res := domain.TeachbackResult{Topic: topic, Title: rub.Title, DocID: rub.Doc}
	for _, c := range rub.Concepts {
		cr := domain.ConceptResult{ID: c.ID, Label: c.Label}
		if covered[c.ID] {
			res.Covered = append(res.Covered, cr)
		} else {
			cr.Hint = c.Hint
			res.Missing = append(res.Missing, cr)
		}
	}
	res.Score = len(res.Covered) * 100 / len(rub.Concepts)
	res.Passed = float64(len(res.Covered)) >= rub.PassRatio*float64(len(rub.Concepts))
	t.card(domain.Card{Type: "teachback", Teachback: &res})
	a.cite(t, rub.Doc)
	a.notif.Track(ctx, t.u, "teachback_graded", map[string]any{"topic": topic, "score": res.Score, "passed": res.Passed, "mode": t.p.Mode})
	if !res.Passed {
		t.newSt = map[string]string{"await": "teachback", "topic": topic, "task": t.state["task"], "acc": textutil.Truncate(explanation, 1500)}
		t.say(fmt.Sprintf("Хорошее начало: раскрыто %d из %d пунктов (%d%%). Чтобы засчитать, нужно хотя бы %d. Подсказки — в карточке. Допиши, что упустил — я учту всё вместе.",
			len(res.Covered), len(rub.Concepts), res.Score, int(rub.PassRatio*float64(len(rub.Concepts))+0.999)))
		t.suggest("Отмена")
		return
	}
	t.newSt = map[string]string{}
	t.say(fmt.Sprintf("Отлично! Ты раскрыл %d из %d пунктов (%d%%) — этого достаточно, чтобы засчитать «Расскажи о %s».", len(res.Covered), len(rub.Concepts), res.Score, rub.Title))
	taskID := t.state["task"]
	if taskID == "" {
		if task, ok := a.teachTask(ctx, t, topic); ok {
			taskID = task.ID
		}
	}
	if taskID == "" {
		t.say("Этот пункт уже отмечен в чек-листе — можно просто гордиться собой.")
		return
	}
	act, err := a.actions.PlanChecklist(ctx, t.u, t.u, taskID, fmt.Sprintf("Teach-back в Тропе: %d%% рубрики", res.Score))
	if err != nil {
		t.say("Не получилось подготовить отметку в чек-листе — попробуй позже.")
		return
	}
	t.say("Засчитать пункт в чек-листе новичка?")
	t.action(act)
}

// grade returns covered concept ids: LLM when available, keyword rubric otherwise.
func (a *Agent) grade(ctx context.Context, t *turn, rub *catalog.Rubric, text string) map[string]bool {
	covered := map[string]bool{}
	norm := textutil.Normalize(text)
	for _, c := range rub.Concepts {
		if textutil.ContainsAny(norm, c.Keywords) {
			covered[c.ID] = true
		}
	}
	if !a.llmOn(ctx, t.u) {
		return covered
	}
	var concepts []llm.Concept
	for _, c := range rub.Concepts {
		concepts = append(concepts, llm.Concept{ID: c.ID, Label: c.Label})
	}
	g, err := a.llm.Grade(ctx, llm.GradeRequest{Topic: rub.Title, Explanation: text, Concepts: concepts})
	if err != nil {
		a.log.Warn("llm grade failed, keyword rubric", "err", err)
		t.p.Mode = ModeLLMFallback
		return covered
	}
	t.p.Mode = ModeLLM
	llmCovered := map[string]bool{}
	for _, id := range g.Covered {
		llmCovered[id] = true
	}
	return llmCovered
}
