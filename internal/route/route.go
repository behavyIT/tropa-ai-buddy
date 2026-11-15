// Package route turns a route template (from the Avito-inspired checklist) into
// a personal, dependency-aware onboarding map: stages, statuses, the next best
// step with an explanation, what the intern is waiting for and what to do
// meanwhile ("пока ждёшь — сделай").
package route

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"tropa/internal/catalog"
	"tropa/internal/clock"
	"tropa/internal/directory"
	"tropa/internal/domain"
	"tropa/internal/policy"
)

// States is the persistence the engine needs.
type States interface {
	TaskStates(ctx context.Context, userID string) (map[string]domain.TaskState, error)
	AccessStates(ctx context.Context, userID string) (map[string]domain.AccessState, error)
}

// TaskView is a task with its effective status for the viewer.
type TaskView struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Owner        string   `json:"owner"`
	OwnerName    string   `json:"owner_name"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	DueDay       int      `json:"due_day"`
	DueDate      string   `json:"due_date"`
	Overdue      bool     `json:"overdue"`
	BlockedBy    []string `json:"blocked_by,omitempty"`
	WaitingFor   string   `json:"waiting_for,omitempty"`
	Doc          string   `json:"doc,omitempty"`
	DocTitle     string   `json:"doc_title,omitempty"`
	ChecklistRef string   `json:"checklist_ref,omitempty"`
	Systems      []string `json:"systems,omitempty"`
	Topic        string   `json:"topic,omitempty"`
	CanComplete  bool     `json:"can_complete"`
	CanNudge     bool     `json:"can_nudge"`
	CanStart     bool     `json:"can_start"`
	external     bool     // waits on someone else (mentor/lead/approval), not on the intern
}

// StageView is a stage with progress.
type StageView struct {
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	FromDay int        `json:"from_day"`
	ToDay   int        `json:"to_day"`
	Done    int        `json:"done"`
	Total   int        `json:"total"`
	Current bool       `json:"current"`
	Tasks   []TaskView `json:"tasks"`
}

// View is the full personal route.
type View struct {
	UserID       string            `json:"user_id"`
	UserName     string            `json:"user_name"`
	RouteTitle   string            `json:"route_title"`
	Day          int               `json:"day"`
	Today        string            `json:"today"`
	Done         int               `json:"done"`
	Total        int               `json:"total"`
	Percent      int               `json:"percent"`
	Overdue      int               `json:"overdue"`
	Stages       []StageView       `json:"stages"`
	Next         []domain.StepItem `json:"next"`
	Waiting      []domain.StepItem `json:"waiting"`
	WhileWaiting []domain.StepItem `json:"while_waiting"`
}

// Engine builds route views.
type Engine struct {
	cat *catalog.Catalog
	dir *directory.Directory
	pol *policy.Policy
	st  States
}

// New creates an engine.
func New(cat *catalog.Catalog, dir *directory.Directory, pol *policy.Policy, st States) *Engine {
	return &Engine{cat: cat, dir: dir, pol: pol, st: st}
}

// RouteFor returns the route template of an intern.
func (e *Engine) RouteFor(intern *directory.Person) (*catalog.Route, error) {
	team, ok := e.cat.Teams[intern.TeamID]
	if !ok {
		return nil, fmt.Errorf("no team pack for %s", intern.TeamID)
	}
	return e.cat.Routes[team.Route], nil
}

// DueDate returns the calendar date of a due day.
func DueDate(start time.Time, dueDay int) time.Time {
	return clock.StartOfDay(start).AddDate(0, 0, dueDay-1)
}

var ruMonths = []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
var ruWeekdays = []string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}

// FormatDate renders "пт, 13 ноября".
func FormatDate(t time.Time) string {
	t = t.In(clock.MSK)
	return fmt.Sprintf("%s, %d %s", ruWeekdays[t.Weekday()], t.Day(), ruMonths[t.Month()-1])
}

// Build computes the personal route of intern as seen by viewer at now.
func (e *Engine) Build(ctx context.Context, viewer, intern *directory.Person, now time.Time) (View, error) {
	r, err := e.RouteFor(intern)
	if err != nil {
		return View{}, err
	}
	states, err := e.st.TaskStates(ctx, intern.ID)
	if err != nil {
		return View{}, err
	}
	access, err := e.st.AccessStates(ctx, intern.ID)
	if err != nil {
		return View{}, err
	}
	day := clock.DayOfInternship(intern.StartDate, now)
	v := View{UserID: intern.ID, UserName: intern.Name, RouteTitle: r.Title, Day: day, Today: FormatDate(now)}

	// 1. stored / derived base status
	base := map[string]string{}
	for _, t := range r.AllTasks() {
		s := domain.TaskTodo
		if st, ok := states[t.ID]; ok {
			s = st.Status
		}
		if t.Kind == "access" {
			s = accessStatus(t.Systems, access, s)
		}
		base[t.ID] = s
	}
	titles := map[string]catalog.Task{}
	for _, t := range r.AllTasks() {
		titles[t.ID] = t
	}
	unlocks := map[string]int{}
	for _, t := range r.AllTasks() {
		for _, d := range t.DependsOn {
			unlocks[d]++
		}
	}

	var candidates []scored
	for _, s := range r.Stages {
		sv := StageView{ID: s.ID, Title: s.Title, FromDay: s.FromDay, ToDay: s.ToDay, Current: day >= s.FromDay && day <= s.ToDay}
		for _, t := range s.Tasks {
			tv := e.taskView(viewer, intern, t, base, titles, access, day)
			sv.Total++
			if tv.Status == domain.TaskDone {
				sv.Done++
			} else if tv.Overdue && t.Owner == "intern" {
				v.Overdue++
			}
			sv.Tasks = append(sv.Tasks, tv)
			if t.Owner == "intern" && (tv.Status == domain.TaskTodo || tv.Status == domain.TaskInProgress) {
				candidates = append(candidates, scored{tv, score(tv, day, unlocks[t.ID])})
			}
			if t.Owner == "intern" && tv.external && t.DueDay <= day+7 && (tv.Status == domain.TaskWaiting || tv.Status == domain.TaskBlocked) {
				v.Waiting = append(v.Waiting, domain.StepItem{TaskID: tv.ID, Title: tv.Title, Status: tv.Status, Why: tv.WaitingFor, Due: tv.DueDate})
			}
		}
		v.Done += sv.Done
		v.Total += sv.Total
		v.Stages = append(v.Stages, sv)
	}
	if v.Total > 0 {
		v.Percent = v.Done * 100 / v.Total
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	for i, c := range candidates {
		item := domain.StepItem{TaskID: c.tv.ID, Title: c.tv.Title, Status: c.tv.Status, Why: why(c.tv, day, unlocks[c.tv.ID], titles), Due: c.tv.DueDate}
		if i < 3 {
			v.Next = append(v.Next, item)
		} else if len(v.Waiting) > 0 && len(v.WhileWaiting) < 2 && c.tv.DueDay <= day+10 {
			v.WhileWaiting = append(v.WhileWaiting, item)
		}
	}
	if len(v.Waiting) > 0 && len(v.WhileWaiting) == 0 && len(v.Next) > 0 {
		v.WhileWaiting = append(v.WhileWaiting, v.Next[0])
	}
	return v, nil
}

type scored struct {
	tv    TaskView
	score float64
}

func accessStatus(systems []string, access map[string]domain.AccessState, stored string) string {
	if len(systems) == 0 {
		return stored
	}
	granted, pending := 0, 0
	for _, s := range systems {
		switch access[s].Status {
		case domain.AccessGranted:
			granted++
		case domain.AccessPending:
			pending++
		}
	}
	switch {
	case granted == len(systems):
		return domain.TaskDone
	case pending > 0:
		return domain.TaskWaiting
	case stored == domain.TaskDone || stored == domain.TaskWaiting:
		return domain.TaskTodo
	}
	return stored
}

func (e *Engine) taskView(viewer, intern *directory.Person, t catalog.Task, base map[string]string, titles map[string]catalog.Task, access map[string]domain.AccessState, day int) TaskView {
	tv := TaskView{ID: t.ID, Title: t.Title, Owner: t.Owner, Kind: t.Kind, Status: base[t.ID], DueDay: t.DueDay,
		DueDate: FormatDate(DueDate(intern.StartDate, t.DueDay)), Doc: t.Doc, ChecklistRef: t.ChecklistRef, Systems: t.Systems, Topic: t.Topic}
	if d, ok := e.cat.Docs[t.Doc]; ok {
		tv.DocTitle = d.Title
	}
	tv.OwnerName = intern.Name
	switch t.Owner {
	case "mentor":
		if p, ok := e.dir.ByID(intern.MentorID); ok {
			tv.OwnerName = p.Name
		}
	case "lead":
		if p, ok := e.dir.ByID(intern.LeadID); ok {
			tv.OwnerName = p.Name
		}
	}
	if tv.Status != domain.TaskDone {
		var blockers []string
		var waitOn []string
		for _, d := range t.DependsOn {
			if base[d] != domain.TaskDone {
				dt := titles[d]
				blockers = append(blockers, dt.Title)
				if dt.Owner != "intern" {
					waitOn = append(waitOn, fmt.Sprintf("%s — «%s»", e.ownerName(intern, dt.Owner), dt.Title))
					tv.external = true
				} else if dt.Kind == "access" && base[d] == domain.TaskWaiting {
					waitOn = append(waitOn, "согласование доступа ("+strings.Join(dt.Systems, ", ")+")")
					tv.external = true
				} else {
					waitOn = append(waitOn, "«"+dt.Title+"»")
				}
			}
		}
		if len(blockers) > 0 && tv.Status != domain.TaskWaiting {
			tv.Status = domain.TaskBlocked
			tv.BlockedBy = blockers
			tv.WaitingFor = "Ждёт: " + strings.Join(waitOn, "; ")
		}
		if tv.Status == domain.TaskWaiting && t.Kind == "access" {
			var pend []string
			for _, s := range t.Systems {
				if access[s].Status == domain.AccessPending {
					pend = append(pend, s)
				}
			}
			tv.WaitingFor = "Ждёт: согласование руководителем (" + strings.Join(pend, ", ") + ")"
			tv.external = true
		}
		tv.Overdue = t.DueDay < day
	}
	notDone := tv.Status != domain.TaskDone && tv.Status != domain.TaskBlocked
	tv.CanComplete = notDone && e.pol.CanCompleteTask(viewer, intern.ID, t)
	tv.CanStart = viewer.ID == intern.ID && t.Owner == "intern" && tv.Status == domain.TaskTodo
	tv.CanNudge = viewer.ID == intern.ID && t.Owner != "intern" && tv.Status != domain.TaskDone
	return tv
}

func (e *Engine) ownerName(intern *directory.Person, owner string) string {
	id := intern.MentorID
	if owner == "lead" {
		id = intern.LeadID
	}
	if p, ok := e.dir.ByID(id); ok {
		return p.Name
	}
	return owner
}

func score(tv TaskView, day, unlocks int) float64 {
	s := 0.0
	switch {
	case tv.DueDay < day:
		s += 1000 + float64(day-tv.DueDay)
	case tv.DueDay == day:
		s += 500
	}
	if tv.Status == domain.TaskInProgress {
		s += 60
	}
	s += float64(unlocks) * 25
	s -= float64(tv.DueDay)
	return s
}

func why(tv TaskView, day, unlocks int, titles map[string]catalog.Task) string {
	var parts []string
	switch {
	case tv.DueDay < day:
		parts = append(parts, fmt.Sprintf("просрочено на %d дн.", day-tv.DueDay))
	case tv.DueDay == day:
		parts = append(parts, "срок — сегодня")
	case tv.DueDay == day+1:
		parts = append(parts, "срок — завтра")
	default:
		parts = append(parts, "срок — "+tv.DueDate)
	}
	if tv.Status == domain.TaskInProgress {
		parts = append(parts, "ты уже начал")
	}
	if unlocks > 0 {
		var names []string
		for _, t := range titles {
			for _, d := range t.DependsOn {
				if d == tv.ID {
					names = append(names, "«"+t.Title+"»")
				}
			}
		}
		sort.Strings(names)
		if len(names) > 2 {
			names = append(names[:2], fmt.Sprintf("ещё %d", len(names)-2))
		}
		parts = append(parts, "откроет "+strings.Join(names, ", "))
	}
	if tv.Kind == "teachback" {
		parts = append(parts, "можно сдать прямо в чате")
	}
	if tv.Kind == "access" {
		parts = append(parts, "Тропа подготовит заявку")
	}
	return Capitalize(strings.Join(parts, "; "))
}

// Capitalize upper-cases the first rune.
func Capitalize(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}
