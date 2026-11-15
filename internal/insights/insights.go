// Package insights builds «Тропинки» (desire paths) for leads and HR BP: where
// interns actually get stuck compared to the planned route, which questions the
// knowledge base cannot answer, and how much routine the buddy took off mentors.
package insights

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"tropa/internal/apperr"
	"tropa/internal/catalog"
	"tropa/internal/clock"
	"tropa/internal/directory"
	"tropa/internal/domain"
	"tropa/internal/kb"
	"tropa/internal/policy"
	"tropa/internal/route"
	"tropa/internal/store"
	"tropa/internal/textutil"
)

// Assumed minutes of mentor time per avoided routine interaction (documented assumptions).
const (
	MinutesPerSelfServedAnswer = 7
	MinutesPerAction           = 10
	MinutesPerTeachback        = 15
	MinutesPerMergedQuestion   = 7
)

// InternProgress is a row of the team progress table.
type InternProgress struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Team    string   `json:"team"`
	Day     int      `json:"day"`
	Done    int      `json:"done"`
	Total   int      `json:"total"`
	Percent int      `json:"percent"`
	Overdue int      `json:"overdue"`
	Waiting []string `json:"waiting"`
	Next    string   `json:"next"`
}

// Metrics are value metrics of the current cohort.
type Metrics struct {
	ChatTurns          int     `json:"chat_turns"`
	Answered           int     `json:"answered"`
	Unanswered         int     `json:"unanswered"`
	SelfServeRate      int     `json:"self_serve_rate"`
	Helpful            int     `json:"helpful"`
	NotHelpful         int     `json:"not_helpful"`
	Escalated          int     `json:"escalated"`
	Merged             int     `json:"merged"`
	MentorAnswers      int     `json:"mentor_answers"`
	Published          int     `json:"published"`
	ActionsExecuted    int     `json:"actions_executed"`
	ActionsFailed      int     `json:"actions_failed"`
	TeachbacksPassed   int     `json:"teachbacks_passed"`
	TasksDone          int     `json:"tasks_done"`
	PermissionDenied   int     `json:"permission_denied"`
	SensitiveBlocked   int     `json:"sensitive_blocked"`
	MentorMinutesSaved int     `json:"mentor_minutes_saved"`
	LLMShare           int     `json:"llm_share"`
	AvgAccessWaitHours float64 `json:"avg_access_wait_hours"`
}

// Hotspot is a route step where interns ask the most / get stuck.
type Hotspot struct {
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	Questions int    `json:"questions"`
	Waiting   int    `json:"waiting"`
	Overdue   int    `json:"overdue"`
	Advice    string `json:"advice"`
}

// Gap is a knowledge-base gap.
type Gap struct {
	Topic  string `json:"topic"`
	Count  int    `json:"count"`
	Source string `json:"source"` // escalated | unanswered
	Status string `json:"status"`
}

// Report is the full insights page.
type Report struct {
	Scope       string           `json:"scope"`
	Interns     []InternProgress `json:"interns"`
	Metrics     Metrics          `json:"metrics"`
	Hotspots    []Hotspot        `json:"hotspots"`
	Gaps        []Gap            `json:"gaps"`
	Assumptions []string         `json:"assumptions"`
	Synthetic   int              `json:"synthetic_events"`
}

// Service builds reports.
type Service struct {
	st  *store.Store
	clk clock.Clock
	dir *directory.Directory
	cat *catalog.Catalog
	pol *policy.Policy
	rt  *route.Engine
	kb  *kb.Index
}

// New creates the service.
func New(st *store.Store, clk clock.Clock, dir *directory.Directory, cat *catalog.Catalog, pol *policy.Policy, rt *route.Engine, ix *kb.Index) *Service {
	return &Service{st: st, clk: clk, dir: dir, cat: cat, pol: pol, rt: rt, kb: ix}
}

// Team returns progress of interns visible to the viewer.
func (s *Service) Team(ctx context.Context, viewer *directory.Person) ([]InternProgress, error) {
	var out []InternProgress
	now := s.clk.Now()
	for _, in := range s.pol.VisibleInterns(viewer) {
		v, err := s.rt.Build(ctx, viewer, in, now)
		if err != nil {
			return nil, err
		}
		ip := InternProgress{ID: in.ID, Name: in.Name, Team: s.teamName(in.TeamID), Day: v.Day, Done: v.Done, Total: v.Total, Percent: v.Percent, Overdue: v.Overdue}
		for _, w := range v.Waiting {
			ip.Waiting = append(ip.Waiting, w.Title)
		}
		if len(v.Next) > 0 {
			ip.Next = v.Next[0].Title
		}
		out = append(out, ip)
	}
	return out, nil
}

func (s *Service) teamName(id string) string {
	if t, ok := s.cat.Teams[id]; ok {
		return t.Name
	}
	return id
}

// Build creates the report for the viewer's scope.
func (s *Service) Build(ctx context.Context, viewer *directory.Person) (Report, error) {
	teams, ok := s.pol.InsightsScope(viewer)
	if !ok {
		return Report{}, apperr.Forbidden("Аналитика доступна руководителям и HR BP.")
	}
	r := Report{Scope: "Все команды"}
	if teams != nil {
		r.Scope = "Команда «" + s.teamName(teams[0]) + "»"
	}
	var err error
	if r.Interns, err = s.Team(ctx, viewer); err != nil {
		return r, err
	}
	events, err := s.st.ListEvents(ctx, teams)
	if err != nil {
		return r, err
	}
	m := &r.Metrics
	stepQuestions := map[string]int{}
	gapTerms := map[string]int{}
	llmTurns := 0
	var waits []float64
	for _, e := range events {
		synthetic := e.Props["cohort"] == "synthetic"
		if synthetic {
			r.Synthetic++
		}
		switch e.Name {
		case "chat_turn":
			if step, ok := e.Props["step"].(string); ok {
				stepQuestions[step]++
			}
			if synthetic {
				continue
			}
			m.ChatTurns++
			if e.Props["mode"] == "llm" {
				llmTurns++
			}
		case "answer_shown":
			if e.Props["answerable"] == false {
				if terms, ok := e.Props["terms"].([]any); ok && len(terms) > 0 {
					var ts []string
					for _, x := range terms {
						if s, ok := x.(string); ok {
							ts = append(ts, s)
						}
					}
					if len(ts) > 4 {
						ts = ts[:4]
					}
					gapTerms[strings.Join(ts, " ")]++
				}
			}
			if synthetic {
				continue
			}
			if e.Props["answerable"] == true {
				m.Answered++
			} else {
				m.Unanswered++
			}
		case "access_wait":
			if h, ok := e.Props["hours"].(float64); ok {
				waits = append(waits, h)
			}
		}
		if synthetic {
			continue
		}
		switch e.Name {
		case "answer_feedback":
			if e.Props["helpful"] == true {
				m.Helpful++
			} else {
				m.NotHelpful++
			}
		case "question_escalated":
			m.Escalated++
			if e.Props["merged"] == true {
				m.Merged++
			}
		case "mentor_answered":
			m.MentorAnswers++
			if e.Props["published"] == true {
				m.Published++
			}
		case "action_executed":
			m.ActionsExecuted++
		case "action_failed":
			m.ActionsFailed++
		case "teachback_graded":
			if e.Props["passed"] == true {
				m.TeachbacksPassed++
			}
		case "task_done":
			m.TasksDone++
		case "permission_denied":
			m.PermissionDenied++
		case "sensitive_blocked":
			m.SensitiveBlocked++
		}
	}
	if total := m.Answered + m.Unanswered; total > 0 {
		m.SelfServeRate = m.Answered * 100 / total
	}
	if m.ChatTurns > 0 {
		m.LLMShare = llmTurns * 100 / m.ChatTurns
	}
	if len(waits) > 0 {
		sum := 0.0
		for _, w := range waits {
			sum += w
		}
		m.AvgAccessWaitHours = float64(int(sum/float64(len(waits))*10)) / 10
	}
	m.MentorMinutesSaved = m.Answered*MinutesPerSelfServedAnswer + m.ActionsExecuted*MinutesPerAction + m.TeachbacksPassed*MinutesPerTeachback + m.Merged*MinutesPerMergedQuestion
	r.Hotspots = s.hotspots(stepQuestions, r.Interns)
	r.Gaps, err = s.gaps(ctx, viewer, teams, gapTerms)
	if err != nil {
		return r, err
	}
	r.Assumptions = []string{
		fmt.Sprintf("Самостоятельный ответ экономит наставнику ≈%d мин, выполненное действие ≈%d мин, принятый teach-back ≈%d мин, объединённый вопрос ≈%d мин.", MinutesPerSelfServedAnswer, MinutesPerAction, MinutesPerTeachback, MinutesPerMergedQuestion),
		"«Тропинки» и пробелы базы учитывают синтетическую прошлую когорту — так дашборд показателен с первого дня демо.",
		"Тексты переписки не попадают в аналитику: для тем без ответа сохраняются только нормализованные термины.",
	}
	return r, nil
}

var advice = map[string]string{
	"access-k8s":      "Ожидание согласования k8s-staging — главный блокер. Кандидат на автовыдачу стажёрам в рамках роли.",
	"avito-cli":       "Много вопросов по настройке avito-cli — стоит добавить пошаговую инструкцию в командный пак.",
	"first-task":      "Первая задача рождает вопросы про ветки и MR — помогут короткий гайд и парное программирование на 1:1.",
	"access-standard": "Стандартные доступы выдаются сразу — вопросов мало, сценарий работает.",
	"meet-team":       "Знакомство с командой ждёт добавления в чаты — Тропа напоминает наставнику автоматически.",
	"teach-sentry":    "Teach-back по Sentry проходит со 2-й попытки — проверьте рубрику и материалы.",
	"stash-keys":      "Вопросы про SSH-ключи — добавьте скриншоты в инструкцию.",
}

func (s *Service) hotspots(stepQuestions map[string]int, interns []InternProgress) []Hotspot {
	titles := map[string]string{}
	for _, r := range s.cat.Routes {
		for _, t := range r.AllTasks() {
			titles[t.ID] = t.Title
		}
	}
	waiting := map[string]int{}
	overdue := map[string]int{}
	for _, in := range interns {
		p, _ := s.dir.ByID(in.ID)
		v, err := s.rt.Build(context.Background(), p, p, s.clk.Now())
		if err != nil {
			continue
		}
		for _, st := range v.Stages {
			for _, t := range st.Tasks {
				if t.Status == domain.TaskWaiting || t.Status == domain.TaskBlocked {
					waiting[t.ID]++
				}
				if t.Overdue && t.Status != domain.TaskDone && t.Owner == "intern" {
					overdue[t.ID]++
				}
			}
		}
	}
	ids := map[string]bool{}
	for id := range stepQuestions {
		ids[id] = true
	}
	for id := range waiting {
		ids[id] = true
	}
	var out []Hotspot
	for id := range ids {
		if titles[id] == "" {
			continue
		}
		out = append(out, Hotspot{TaskID: id, Title: titles[id], Questions: stepQuestions[id], Waiting: waiting[id], Overdue: overdue[id], Advice: advice[id]})
	}
	sort.Slice(out, func(i, j int) bool {
		si := out[i].Questions + 3*out[i].Waiting + 5*out[i].Overdue
		sj := out[j].Questions + 3*out[j].Waiting + 5*out[j].Overdue
		if si != sj {
			return si > sj
		}
		return out[i].TaskID < out[j].TaskID
	})
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func (s *Service) gaps(ctx context.Context, viewer *directory.Person, teams []string, terms map[string]int) ([]Gap, error) {
	var out []Gap
	scope := teams
	if scope == nil {
		for id := range s.cat.Teams {
			scope = append(scope, id)
		}
	}
	qs, err := s.st.QuestionsForTeams(ctx, scope)
	if err != nil {
		return nil, err
	}
	for _, q := range qs {
		status := "ждёт ответа"
		if q.Status == domain.ExtAnswered {
			status = "отвечен"
			if q.CardID != "" {
				status = "отвечен и добавлен в базу"
			}
		}
		out = append(out, Gap{Topic: q.Text, Count: len(q.Askers), Source: "escalated", Status: status})
	}
	for t, n := range terms {
		dup := false
		for _, q := range qs { // already escalated to a mentor → shown once, with its status
			if textutil.Jaccard(strings.Fields(t), textutil.Terms(q.Text)) >= 0.5 {
				dup = true
				break
			}
		}
		if !dup {
			status := "нет в базе"
			if s.kb != nil && kb.Answerable(s.kb.Search(viewer, t, 1)) {
				status = "закрыт ответом в базе" // e.g. a mentor answer published since then
			}
			out = append(out, Gap{Topic: t, Count: n, Source: "unanswered", Status: status})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > 8 {
		out = out[:8]
	}
	return out, nil
}
