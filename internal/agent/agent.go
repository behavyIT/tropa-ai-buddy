// Package agent is the buddy's orchestrator: a deterministic state machine
// that decides what to do, with the LLM as an optional language layer. The
// design follows the SDD specs in openspec/specs: permissions are enforced in
// code, side effects are only *planned* (the user confirms), and every answer
// is grounded in ACL-filtered sources with citations.
package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"tropa/internal/actions"
	"tropa/internal/apperr"
	"tropa/internal/catalog"
	"tropa/internal/clock"
	"tropa/internal/directory"
	"tropa/internal/domain"
	"tropa/internal/flags"
	"tropa/internal/kb"
	"tropa/internal/llm"
	"tropa/internal/nlu"
	"tropa/internal/notify"
	"tropa/internal/policy"
	"tropa/internal/route"
	"tropa/internal/store"
)

// MaxMessageLen bounds user input.
const MaxMessageLen = 2000

// Modes of answering.
const (
	ModeOffline     = "offline"
	ModeLLM         = "llm"
	ModeLLMFallback = "llm_fallback"
)

// Agent orchestrates a chat turn.
type Agent struct {
	st      *store.Store
	clk     clock.Clock
	dir     *directory.Directory
	cat     *catalog.Catalog
	pol     *policy.Policy
	kb      *kb.Index
	route   *route.Engine
	parser  *nlu.Parser
	llm     llm.Provider
	flags   *flags.Service
	actions *actions.Service
	notif   *notify.Notifier
	log     *slog.Logger
}

// Deps groups agent dependencies.
type Deps struct {
	Store   *store.Store
	Clock   clock.Clock
	Dir     *directory.Directory
	Catalog *catalog.Catalog
	Policy  *policy.Policy
	KB      *kb.Index
	Route   *route.Engine
	LLM     llm.Provider // nil = offline only
	Flags   *flags.Service
	Actions *actions.Service
	Notify  *notify.Notifier
	Log     *slog.Logger
}

// New creates an agent.
func New(d Deps) *Agent {
	return &Agent{st: d.Store, clk: d.Clock, dir: d.Dir, cat: d.Catalog, pol: d.Policy, kb: d.KB, route: d.Route, parser: nlu.New(d.Catalog),
		llm: d.LLM, flags: d.Flags, actions: d.Actions, notif: d.Notify, log: d.Log}
}

// Reply is the result of a chat turn.
type Reply struct {
	User    domain.Message  `json:"user"`
	Buddy   domain.Message  `json:"buddy"`
	Actions []domain.Action `json:"actions"`
}

// turn accumulates the buddy response.
type turn struct {
	u       *directory.Person
	text    string
	norm    string
	now     time.Time
	state   map[string]string
	out     strings.Builder
	p       domain.MessagePayload
	actions []domain.Action
	newSt   map[string]string
}

func (t *turn) say(s string) {
	if t.out.Len() > 0 {
		t.out.WriteString("\n\n")
	}
	t.out.WriteString(s)
}

func (t *turn) card(c domain.Card) { t.p.Cards = append(t.p.Cards, c) }

func (t *turn) action(a domain.Action) {
	t.actions = append(t.actions, a)
	t.card(domain.Card{Type: "action", ActionID: a.ID})
}

func (t *turn) suggest(s ...string) { t.p.Suggestions = append(t.p.Suggestions, s...) }

// History returns the user's recent chat.
func (a *Agent) History(ctx context.Context, u *directory.Person) ([]domain.Message, error) {
	return a.st.ListMessages(ctx, u.ID, 100)
}

// Handle processes a user message end-to-end.
func (a *Agent) Handle(ctx context.Context, u *directory.Person, text string) (Reply, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Reply{}, apperr.BadRequest("Пустое сообщение.")
	}
	if utf8.RuneCountInString(text) > MaxMessageLen {
		return Reply{}, apperr.BadRequest("Слишком длинное сообщение (максимум %d символов).", MaxMessageLen)
	}
	now := a.clk.Now()
	userMsg := domain.Message{ID: uuid.NewString(), UserID: u.ID, Role: "user", Text: text, CreatedAt: now}
	if err := a.st.InsertMessage(ctx, userMsg); err != nil {
		return Reply{}, err
	}
	state, err := a.st.GetChatState(ctx, u.ID)
	if err != nil {
		return Reply{}, err
	}
	t := &turn{u: u, text: text, norm: strings.ToLower(text), now: now, state: state}
	t.p.Mode = ModeOffline
	a.dispatch(ctx, t)
	if t.newSt != nil {
		if err := a.st.SetChatState(ctx, u.ID, t.newSt, now); err != nil {
			return Reply{}, err
		}
	}
	buddy := domain.Message{ID: uuid.NewString(), UserID: u.ID, Role: "buddy", Text: t.out.String(), Payload: t.p, CreatedAt: a.clk.Now().Add(time.Millisecond)}
	if err := a.st.InsertMessage(ctx, buddy); err != nil {
		return Reply{}, err
	}
	props := map[string]any{"intent": t.p.Intent, "mode": t.p.Mode, "actions": len(t.actions)}
	if u.Role == directory.RoleIntern {
		if v, err := a.route.Build(ctx, u, u, now); err == nil && len(v.Next) > 0 {
			props["step"] = v.Next[0].TaskID // where the intern is on the route when asking
		}
	}
	a.notif.Track(ctx, u, "chat_turn", props)
	return Reply{User: userMsg, Buddy: buddy, Actions: t.actions}, nil
}

func isCancel(norm string) bool {
	norm = strings.Trim(strings.TrimSpace(norm), ".!")
	for _, w := range []string{"отмена", "отмени", "стоп", "не надо", "передумал", "выйти", "cancel"} {
		if norm == w || strings.HasPrefix(norm, w+" ") {
			return true
		}
	}
	return false
}

func (a *Agent) dispatch(ctx context.Context, t *turn) {
	// 1. multi-turn states
	switch t.state["await"] {
	case "teachback":
		if isCancel(t.norm) {
			t.newSt = map[string]string{}
			t.p.Intent = "teachback_cancel"
			t.say("Хорошо, вернёмся к этому позже. Пункт останется в маршруте.")
			t.suggest("Что мне делать дальше?")
			return
		}
		u := a.parser.Parse(t.text, t.now)
		if u.Intent == nlu.IntentQuestion || u.Intent == nlu.IntentTeachback || u.Intent == nlu.IntentCompleteTask {
			a.gradeTeachback(ctx, t)
			return
		}
		t.newSt = map[string]string{} // user switched topic
	case "mentor_question":
		t.newSt = map[string]string{}
		if isCancel(t.norm) {
			t.p.Intent = "ask_mentor_cancel"
			t.say("Ок, не отправляю.")
			return
		}
		a.planQuestion(ctx, t, t.text)
		return
	}
	// 2. understanding: deterministic first, LLM refines when available
	und := a.understand(ctx, t)
	t.p.Intent = und.Intent
	if und.Sub != "" {
		t.p.Intent += "." + und.Sub
	}
	switch und.Intent {
	case nlu.IntentSensitive:
		a.handleSensitive(ctx, t)
	case nlu.IntentGreeting:
		a.handleGreeting(ctx, t)
	case nlu.IntentThanks:
		t.say("Пожалуйста! Если что-то ещё — я рядом. Заглядывай в маршрут, там видно следующий шаг.")
		t.suggest("Что мне делать дальше?")
	case nlu.IntentNextStep:
		a.handleNextStep(ctx, t)
	case nlu.IntentAbsence:
		a.handleAbsence(ctx, t, und)
	case nlu.IntentAccess:
		a.handleAccess(ctx, t, und)
	case nlu.IntentWho:
		a.handleWho(ctx, t)
	case nlu.IntentAskMentor:
		a.handleAskMentor(ctx, t)
	case nlu.IntentTeachback:
		a.startTeachback(ctx, t, und.Topic)
	case nlu.IntentStatus:
		a.handleStatus(ctx, t)
	case nlu.IntentCompleteTask:
		a.handleComplete(ctx, t)
	default:
		a.handleQuestion(ctx, t)
	}
}

// understand runs the deterministic parser and, if enabled, the LLM.
func (a *Agent) understand(ctx context.Context, t *turn) nlu.Understanding {
	det := a.parser.Parse(t.text, t.now)
	if det.Intent == nlu.IntentSensitive || det.Confidence >= 0.85 || !a.llmOn(ctx, t.u) {
		return det
	}
	var systems []string
	for _, s := range a.cat.Access {
		systems = append(systems, s.ID)
	}
	var topics []string
	for k := range a.cat.Rubrics {
		topics = append(topics, k)
	}
	team := ""
	if tp, ok := a.cat.Teams[t.u.TeamID]; ok {
		team = tp.Name
	}
	res, err := a.llm.Understand(ctx, llm.UnderstandRequest{Text: t.text, Today: t.now.Format("2006-01-02 (Monday)"), UserRole: t.u.Role, TeamName: team,
		Systems: systems, Topics: topics, Intents: nlu.AllIntents})
	if err != nil {
		a.log.Warn("llm understand failed, using offline parser", "err", err)
		t.p.Mode = ModeLLMFallback
		return det
	}
	t.p.Mode = ModeLLM
	return mergeUnderstanding(det, res)
}

func mergeUnderstanding(det nlu.Understanding, l llm.Understanding) nlu.Understanding {
	valid := false
	for _, i := range nlu.AllIntents {
		if i == l.Intent {
			valid = true
		}
	}
	if !valid || l.Intent == nlu.IntentSensitive && det.Intent != nlu.IntentSensitive && l.Confidence < 0.5 {
		return det
	}
	out := nlu.Understanding{Intent: l.Intent, Sub: l.Sub, Topic: l.Topic, Standard: l.Standard || det.Standard, Confidence: l.Confidence}
	out.Systems = l.Systems
	if len(out.Systems) == 0 {
		out.Systems = det.Systems
	}
	out.DateFrom, out.DateTo = det.DateFrom, det.DateTo
	if f, err := time.ParseInLocation("2006-01-02", l.DateFrom, clock.MSK); err == nil {
		out.DateFrom = &f
		to := f
		if t2, err := time.ParseInLocation("2006-01-02", l.DateTo, clock.MSK); err == nil {
			to = t2
		}
		out.DateTo = &to
	}
	if out.Intent == nlu.IntentAbsence && out.DateFrom != nil {
		out.Sub = nlu.SubDo
	}
	if out.Intent == nlu.IntentTeachback && out.Topic == "" {
		out.Topic = det.Topic
	}
	return out
}

func (a *Agent) llmOn(ctx context.Context, u *directory.Person) bool {
	return a.llm != nil && a.flags.Enabled(ctx, flags.LLM, u.TeamID)
}

func (a *Agent) mentorOf(u *directory.Person) *directory.Person {
	if m, ok := a.dir.ByID(u.MentorID); ok {
		return m
	}
	return nil
}

func (a *Agent) leadOf(u *directory.Person) *directory.Person {
	if l, ok := a.dir.LeadOf(u.TeamID); ok && l.ID != u.ID {
		return l
	}
	return nil
}
