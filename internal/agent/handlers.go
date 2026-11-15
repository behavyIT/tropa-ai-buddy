package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"tropa/internal/actions"
	"tropa/internal/apperr"
	"tropa/internal/clock"
	"tropa/internal/directory"
	"tropa/internal/domain"
	"tropa/internal/flags"
	"tropa/internal/kb"
	"tropa/internal/llm"
	"tropa/internal/nlu"
	"tropa/internal/route"
	"tropa/internal/textutil"
)

func (a *Agent) handleSensitive(ctx context.Context, t *turn) {
	t.say("Это чувствительная тема (зарплаты, здоровье, документы, финансы или личные данные коллег). С такими вопросами я не работаю — чтобы случайно ничего не раскрыть и не дать неточный ответ.")
	if hr, ok := a.dir.HRBP(); ok {
		t.say(fmt.Sprintf("Лучше напрямую к HR BP: %s, %s. Если неловко писать — можно начать с «Подскажите, к кому обратиться по вопросу…».", hr.Name, hr.Contact))
		t.card(domain.Card{Type: "person", Person: &domain.PersonCard{ID: hr.ID, Name: hr.Name, Title: hr.Title, Contact: hr.Contact, Topic: "Чувствительные вопросы"}})
	}
	a.notif.Track(ctx, t.u, "sensitive_blocked", nil)
}

func (a *Agent) handleGreeting(ctx context.Context, t *turn) {
	if t.u.Role != directory.RoleIntern {
		t.say(fmt.Sprintf("Привет, %s! Я Тропа — AI-бадди стажёров. Могу ответить на вопросы по материалам команды, подсказать, к кому обратиться, и оформить отгул.", t.u.FirstName()))
		t.suggest("Как оформить отгул?", "Как проходит ревью компетенций?")
		return
	}
	v, err := a.route.Build(ctx, t.u, t.u, t.now)
	if err != nil {
		t.say("Привет! Я Тропа — твой AI-бадди.")
		return
	}
	t.say(fmt.Sprintf("Привет, %s! Я Тропа — твой AI-бадди. Сегодня %d-й день стажировки, пройдено %d из %d шагов маршрута.", t.u.FirstName(), v.Day, v.Done, v.Total))
	t.say("Спрашивай что угодно про команду и инструменты. Ещё я могу оформить отгул, запросить доступы и принять у тебя «Расскажи о …» из чек-листа.")
	if len(v.Next) > 0 {
		t.say("Вот с чего я бы начал:")
		t.card(domain.Card{Type: "steps", Title: "Следующий шаг", Steps: v.Next})
	}
	t.suggest(a.suggestionsFor(v)...)
}

func (a *Agent) suggestionsFor(v route.View) []string {
	out := []string{"Что мне делать дальше?"}
	for _, s := range v.Next {
		switch {
		case strings.HasPrefix(s.TaskID, "access-standard"):
			out = append(out, "Запроси стандартные доступы")
		case s.TaskID == "access-k8s":
			out = append(out, "Нужен доступ к k8s-staging")
		case strings.HasPrefix(s.TaskID, "teach-"):
			out = append(out, "Хочу рассказать про "+strings.TrimPrefix(s.Title, "Расскажи о "))
		}
	}
	out = append(out, "Как оформить отгул?")
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

func (a *Agent) handleNextStep(ctx context.Context, t *turn) {
	if t.u.Role != directory.RoleIntern {
		t.say("Маршрут адаптации есть только у стажёров. Прогресс подопечных видно во вкладке «Команда».")
		return
	}
	v, err := a.route.Build(ctx, t.u, t.u, t.now)
	if err != nil {
		t.say("Не получилось построить маршрут — попробуй обновить страницу.")
		return
	}
	line := fmt.Sprintf("День %d · %s. Пройдено %d из %d (%d%%).", v.Day, v.Today, v.Done, v.Total, v.Percent)
	if v.Overdue > 0 {
		line += fmt.Sprintf(" Просроченных пунктов: %d — начнём с них.", v.Overdue)
	}
	t.say(line)
	if len(v.Next) > 0 {
		t.card(domain.Card{Type: "steps", Title: "Следующий шаг", Steps: v.Next})
	} else {
		t.say("Все доступные пункты на сегодня закрыты. Отличная работа!")
	}
	if len(v.Waiting) > 0 {
		t.card(domain.Card{Type: "steps", Title: "Ждём", Steps: v.Waiting})
		if len(v.WhileWaiting) > 0 {
			t.card(domain.Card{Type: "steps", Title: "Пока ждёшь — можно сделать", Steps: v.WhileWaiting})
		}
	}
	t.suggest(a.suggestionsFor(v)[1:]...)
}

var reHowAbsence = regexp.MustCompile(`отвлек|руковод|предупред|писать|сообщ`)

func (a *Agent) handleAbsence(ctx context.Context, t *turn, und nlu.Understanding) {
	lead, mentor := a.leadOf(t.u), a.mentorOf(t.u)
	if !a.flags.Enabled(ctx, flags.ActionsAbsence, t.u.TeamID) {
		a.explainAbsence(t, lead, mentor)
		t.say("Оформление через Тропу в твоей команде сейчас выключено — подай заявку в сервисе отсутствий напрямую.")
		return
	}
	if und.Sub != nlu.SubDo || und.DateFrom == nil {
		if und.Sub == nlu.SubDo {
			t.say("На какую дату оформить отгул? Напиши, например, «на пятницу» или «с 13 по 14 ноября».")
		} else {
			a.explainAbsence(t, lead, mentor)
			if reHowAbsence.MatchString(t.norm) && lead != nil {
				t.say(fmt.Sprintf("Про «отвлекать ли руководителя»: писать лично не обязательно — заявка сама придёт на согласование (%s). Если отгул срочный или совпадает с важной встречей, коротко предупреди руководителя в личке — это нормально [1].", lead.Name))
			}
			t.say("Могу подготовить заявку — просто напиши дату.")
		}
		t.suggest("Оформи отгул на "+dateWords(clock.AddWorkdays(t.now, 2)), "Оформи отгул на "+dateWords(nextWeekday(t.now, time.Friday)))
		return
	}
	act, err := a.actions.PlanAbsence(ctx, t.u, *und.DateFrom, *und.DateTo, "")
	if err != nil {
		if e, ok := apperr.As(err); ok {
			t.say(e.Message)
			return
		}
		a.log.Error("plan absence", "err", err)
		t.say("Не получилось подготовить заявку — попробуй ещё раз.")
		return
	}
	approver := "руководитель"
	if lead != nil {
		approver = lead.Name
	}
	t.say(fmt.Sprintf("Подготовил заявку на отгул: %s. Проверь детали и подтверди — отправлю в сервис отсутствий, согласует %s.", actions.FormatRange(*und.DateFrom, *und.DateTo), approver))
	if len(act.View.Warnings) > 0 {
		t.say("Обрати внимание — есть предупреждения, они в карточке.")
	}
	t.action(act)
	a.cite(t, "absence-dayoff")
}

// dateWords renders "13 ноября" — a form the offline parser understands back.
func dateWords(t time.Time) string {
	months := []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
	return fmt.Sprintf("%d %s", t.Day(), months[t.Month()-1])
}

func nextWeekday(now time.Time, wd time.Weekday) time.Time {
	d := clock.StartOfDay(now)
	delta := (int(wd) - int(d.Weekday()) + 7) % 7
	if delta == 0 {
		delta = 7
	}
	return d.AddDate(0, 0, delta)
}

func (a *Agent) explainAbsence(t *turn, lead, mentor *directory.Person) {
	leadName, mentorName := "руководитель", "наставника"
	if lead != nil {
		leadName = lead.Name
	}
	if mentor != nil {
		mentorName = mentor.Name
	}
	t.say(fmt.Sprintf("Отгул оформляется заявкой в сервисе отсутствий, согласует руководитель — %s. %s достаточно предупредить, отдельное разрешение не нужно [1].\n\n"+
		"Порядок: выбери даты (по тестовому правилу — минимум за 2 рабочих дня и не больше 3 дней подряд), проверь календарь команды на демо и планирования, создай заявку, дождись согласования [1].",
		leadName, route.Capitalize(mentorNameAcc(mentorName))))
	a.cite(t, "absence-dayoff")
}

func mentorNameAcc(n string) string {
	if n == "наставника" {
		return n
	}
	return "наставника (" + n + ")"
}

func (a *Agent) cite(t *turn, docID string) {
	for _, c := range t.p.Citations {
		if c.DocID == docID {
			return
		}
	}
	_, text, ok := a.kb.DocText(t.u, docID)
	if !ok {
		return
	}
	t.p.Citations = append(t.p.Citations, domain.Citation{N: len(t.p.Citations) + 1, DocID: docID, Title: a.kb.DocTitle(docID), Kind: kb.KindInstruction, Snippet: textutil.Truncate(stripHeadings(text), 220)})
}

func stripHeadings(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "## ") || strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, " ")
}

func (a *Agent) handleAccess(ctx context.Context, t *turn, und nlu.Understanding) {
	if und.Sub == nlu.SubHow || !a.flags.Enabled(ctx, flags.ActionsAccess, t.u.TeamID) {
		a.answerFrom(ctx, t, "access-overview")
		if !a.flags.Enabled(ctx, flags.ActionsAccess, t.u.TeamID) {
			t.say("Заявки через Тропу в твоей команде сейчас выключены — подай их в сервисе доступов напрямую.")
		} else {
			t.suggest("Запроси стандартные доступы")
		}
		return
	}
	systems := und.Systems
	if und.Standard || len(systems) == 0 {
		for _, s := range a.cat.Access {
			if s.Standard {
				systems = append(systems, s.ID)
			}
		}
		if r, err := a.route.RouteFor(t.u); err == nil {
			if task, ok := r.Task("access-standard"); ok {
				systems = task.Systems
			}
		}
	}
	res, err := a.actions.PlanAccess(ctx, t.u, systems, accessJustification(a, t))
	if err != nil {
		if e, ok := apperr.As(err); ok {
			t.say(e.Message)
			return
		}
		a.log.Error("plan access", "err", err)
		t.say("Не получилось подготовить заявку — попробуй ещё раз.")
		return
	}
	for _, f := range res.Forbidden {
		t.say("Не могу запросить: " + f + ". Если для задачи нужны такие данные — обсуди с наставником: обычно хватает staging или обезличенной выгрузки [1].")
		a.cite(t, "access-overview")
		a.notif.Track(ctx, t.u, "permission_denied", map[string]any{"what": "access"})
	}
	if len(res.Granted) > 0 {
		t.say("Уже есть: " + strings.Join(res.Granted, ", ") + " ✅")
	}
	if len(res.Pending) > 0 {
		t.say("Уже на согласовании: " + strings.Join(res.Pending, ", ") + ". Я напомню, когда решат.")
	}
	if res.Action != nil {
		t.say("Подготовил заявку в сервис доступов. Проверь список и подтверди — стандартные доступы выдадут сразу, остальные уйдут на согласование.")
		t.action(*res.Action)
		a.cite(t, "access-overview")
	} else if len(res.Forbidden) == 0 && len(res.Granted)+len(res.Pending) > 0 {
		t.suggest("Что мне делать дальше?")
	}
}

func accessJustification(a *Agent, t *turn) string {
	return "Онбординг: доступы по маршруту адаптации (" + t.u.Title + ")"
}

func (a *Agent) answerFrom(ctx context.Context, t *turn, docID string) {
	title, text, ok := a.kb.DocText(t.u, docID)
	if !ok {
		a.handleQuestion(ctx, t)
		return
	}
	t.say(textutil.Truncate(stripHeadingsKeepLines(text), 900) + " [1]")
	t.p.Citations = append(t.p.Citations, domain.Citation{N: 1, DocID: docID, Title: title, Kind: kb.KindInstruction, Snippet: textutil.Truncate(stripHeadings(text), 220)})
}

func stripHeadingsKeepLines(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "## ") {
			continue
		}
		out = append(out, l)
	}
	return strings.TrimSpace(regexp.MustCompile(`\n{3,}`).ReplaceAllString(strings.Join(out, "\n"), "\n\n"))
}

func (a *Agent) handleWho(ctx context.Context, t *turn) {
	norm := textutil.Normalize(t.text)
	switch {
	case strings.Contains(norm, "наставник") || strings.Contains(norm, "бадди"):
		if m := a.mentorOf(t.u); m != nil {
			t.say(fmt.Sprintf("Твой наставник — %s, %s. С ним — вопросы по коду, задачам и инструментам.", m.Name, strings.ToLower(m.Title)))
			t.card(domain.Card{Type: "person", Person: &domain.PersonCard{ID: m.ID, Name: m.Name, Title: m.Title, Contact: m.Contact, Topic: "Наставник"}})
			return
		}
	case strings.Contains(norm, "руководител"):
		if l := a.leadOf(t.u); l != nil {
			t.say(fmt.Sprintf("Руководитель твоей команды — %s. К ней — отгулы, доступы с согласованием и приоритеты.", l.Name))
			t.card(domain.Card{Type: "person", Person: &domain.PersonCard{ID: l.ID, Name: l.Name, Title: l.Title, Contact: l.Contact, Topic: "Руководитель"}})
			return
		}
	}
	team, ok := a.cat.Teams[t.u.TeamID]
	best, bestScore := -1, 0
	if ok {
		for i, c := range team.Contacts {
			score := 0
			for _, k := range c.Keywords {
				if strings.Contains(norm, k) {
					score++
				}
			}
			if score > bestScore {
				best, bestScore = i, score
			}
		}
	}
	if best < 0 {
		t.say("Не нашёл точного контакта по этой теме. Начни с наставника — он подскажет, к кому идти. Могу передать ему вопрос.")
		if m := a.mentorOf(t.u); m != nil {
			t.card(domain.Card{Type: "person", Person: &domain.PersonCard{ID: m.ID, Name: m.Name, Title: m.Title, Contact: m.Contact, Topic: "Наставник"}})
		}
		t.suggest("Спроси наставника: " + textutil.Truncate(t.text, 80))
		return
	}
	c := team.Contacts[best]
	p, _ := a.dir.ByID(c.Person)
	t.say(fmt.Sprintf("По теме «%s» — %s. %s", c.Topic, p.Name, c.How))
	t.card(domain.Card{Type: "person", Person: &domain.PersonCard{ID: p.ID, Name: p.Name, Title: p.Title, Contact: p.Contact, Topic: c.Topic, How: c.How, Channel: c.Channel}})
	if p.ID == t.u.MentorID {
		t.suggest("Спроси наставника: " + textutil.Truncate(t.text, 80))
	}
}

var reAskPrefix = regexp.MustCompile(`(?i)^\s*(спроси|передай|напиши|задай вопрос|вопрос)\s*(у\s+)?(наставник[ау]?|бадди|ментор[ау]?)\s*[:,—-]?\s*`)

func (a *Agent) handleAskMentor(ctx context.Context, t *turn) {
	if !a.pol.CanAskMentor(t.u) {
		t.say("Передавать вопросы наставнику могут стажёры с назначенным наставником.")
		return
	}
	q := strings.TrimSpace(reAskPrefix.ReplaceAllString(t.text, ""))
	if len(strings.Fields(q)) < 3 {
		t.newSt = map[string]string{"await": "mentor_question"}
		m := a.mentorOf(t.u)
		t.say(fmt.Sprintf("Сформулируй вопрос одним сообщением — я покажу, что уйдёт %s, и отправлю после подтверждения.", dative(m)))
		t.suggest("Отмена")
		return
	}
	a.planQuestion(ctx, t, q)
}

func dative(m *directory.Person) string {
	if m == nil {
		return "наставнику"
	}
	return "наставнику (" + m.Name + ")"
}

var reUrgent = regexp.MustCompile(`срочн|горит|блокер|не могу работать|прямо сейчас|asap`)

func (a *Agent) planQuestion(ctx context.Context, t *turn, q string) {
	ctxLine := a.contextLine(ctx, t)
	act, err := a.actions.PlanQuestion(ctx, t.u, textutil.Truncate(q, 600), ctxLine, reUrgent.MatchString(strings.ToLower(q)))
	if err != nil {
		if e, ok := apperr.As(err); ok {
			t.say(e.Message)
			return
		}
		t.say("Не получилось подготовить вопрос — попробуй ещё раз.")
		return
	}
	m := a.mentorOf(t.u)
	t.say(fmt.Sprintf("Подготовил вопрос %s. Он увидит только этот вопрос и контекст, а не нашу переписку. Подтверди отправку.", dative(m)))
	t.action(act)
}

func (a *Agent) contextLine(ctx context.Context, t *turn) string {
	v, err := a.route.Build(ctx, t.u, t.u, t.now)
	if err != nil {
		return ""
	}
	line := fmt.Sprintf("День %d стажировки", v.Day)
	if len(v.Next) > 0 {
		line += "; текущий шаг: «" + v.Next[0].Title + "»"
	}
	return line
}

func (a *Agent) handleStatus(ctx context.Context, t *turn) {
	acts, err := a.actions.List(ctx, t.u)
	if err != nil {
		t.say("Не получилось загрузить заявки — попробуй ещё раз.")
		return
	}
	var ids []string
	var lines []string
	for _, act := range acts {
		if act.Status == domain.ActionPlanned && act.Type != domain.ActionAbsence && act.Type != domain.ActionAccess {
			continue
		}
		ids = append(ids, act.ID)
		lines = append(lines, "• "+act.Title+" — "+StatusLabel(act))
		if len(ids) == 5 {
			break
		}
	}
	if len(ids) == 0 {
		t.say("У тебя пока нет заявок. Могу оформить отгул или запросить доступы.")
		t.suggest("Запроси стандартные доступы", "Как оформить отгул?")
		return
	}
	t.say("Твои заявки:\n" + strings.Join(lines, "\n"))
	t.card(domain.Card{Type: "requests", ActionIDs: ids})
}

// StatusLabel renders a human status of an action.
func StatusLabel(a domain.Action) string {
	switch a.Status {
	case domain.ActionPlanned:
		return "ждёт твоего подтверждения"
	case domain.ActionExecuting:
		return "отправляется"
	case domain.ActionFailed:
		return "не отправлена: " + a.Error
	case domain.ActionCancelled:
		return "отменена"
	}
	switch a.ExternalStatus {
	case domain.ExtPendingApproval:
		return "на согласовании"
	case domain.ExtPartial:
		return "частично выдано, остальное на согласовании"
	case domain.ExtApproved:
		return "согласована ✅"
	case domain.ExtGranted:
		return "выдано ✅"
	case domain.ExtRejected:
		return "отклонена"
	case domain.ExtCompleted:
		return "отмечено ✅"
	case domain.ExtOpen:
		return "передан наставнику, ждём ответ"
	case domain.ExtAnswered:
		return "есть ответ ✅"
	}
	return "выполнено"
}

func (a *Agent) handleComplete(ctx context.Context, t *turn) {
	if t.u.Role != directory.RoleIntern {
		a.handleQuestion(ctx, t)
		return
	}
	v, err := a.route.Build(ctx, t.u, t.u, t.now)
	if err != nil {
		t.say("Не получилось открыть маршрут.")
		return
	}
	terms := textutil.Terms(t.text)
	var best *route.TaskView
	bestScore := 0.0
	for si := range v.Stages {
		for ti := range v.Stages[si].Tasks {
			tv := &v.Stages[si].Tasks[ti]
			if tv.Owner != "intern" || tv.Status == domain.TaskDone {
				continue
			}
			sc := textutil.Jaccard(terms, textutil.Terms(tv.Title))
			if sc > bestScore {
				best, bestScore = tv, sc
			}
		}
	}
	if best == nil || bestScore < 0.15 {
		t.say("Не понял, какой пункт отметить. Вот текущие шаги — выбери нужный:")
		if len(v.Next) > 0 {
			t.card(domain.Card{Type: "steps", Title: "Текущие шаги", Steps: v.Next})
			for _, s := range v.Next {
				t.suggest("Отметь «" + s.Title + "»")
			}
		}
		return
	}
	switch best.Kind {
	case "teachback":
		t.say("Этот пункт засчитывается через «Расскажи мне»: ты объясняешь тему, я проверяю. Начнём?")
		t.suggest("Хочу рассказать про " + strings.TrimPrefix(best.Title, "Расскажи о "))
		return
	case "access":
		t.say("Этот пункт закроется сам, когда сервис доступов выдаст все доступы. Могу подготовить заявку.")
		t.suggest("Запроси стандартные доступы")
		return
	}
	if best.Status == domain.TaskBlocked {
		t.say("Этот пункт пока заблокирован. " + best.WaitingFor)
		return
	}
	act, err := a.actions.PlanChecklist(ctx, t.u, t.u, best.ID, "Стажёр отметил в чате Тропы")
	if err != nil {
		if e, ok := apperr.As(err); ok {
			t.say(e.Message)
			return
		}
		t.say("Не получилось подготовить отметку.")
		return
	}
	t.say("Отмечу в чек-листе новичка «" + best.Title + "». Подтверди:")
	t.action(act)
}

// handleQuestion answers from the permission-filtered knowledge base.
func (a *Agent) handleQuestion(ctx context.Context, t *turn) {
	hits := a.kb.Search(t.u, t.text, 4)
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.Chunk.DocID)
	}
	if !kb.Answerable(hits) {
		a.noAnswer(ctx, t)
		// privacy: store only stemmed terms of unanswered questions, never raw chat text
		a.notif.Track(ctx, t.u, "answer_shown", map[string]any{"answerable": false, "terms": textutil.Terms(t.text)})
		return
	}
	var cited []kb.Hit
	for i, h := range hits {
		if i == 0 || (h.Coverage >= 0.5 && h.Score >= hits[0].Score*0.6 && len(cited) < 3) {
			cited = append(cited, h)
		}
	}
	for i, h := range cited {
		c := domain.Citation{N: i + 1, DocID: h.Chunk.DocID, Title: h.Chunk.Title, Kind: h.Chunk.Kind, Snippet: textutil.Truncate(h.Chunk.Text, 220), Author: h.Chunk.Author}
		if !h.Chunk.Date.IsZero() {
			c.Date = h.Chunk.Date.Format("02.01.2006")
		}
		t.p.Citations = append(t.p.Citations, c)
	}
	answered := false
	if a.llmOn(ctx, t.u) {
		answered = a.composeLLM(ctx, t, cited)
	}
	if !answered {
		a.composeOffline(t, cited)
	}
	if t.u.Role == directory.RoleIntern {
		t.suggest("Спроси наставника: "+textutil.Truncate(t.text, 80), "Что мне делать дальше?")
	}
	a.notif.Track(ctx, t.u, "answer_shown", map[string]any{"answerable": true, "docs": ids, "mode": t.p.Mode})
}

func (a *Agent) composeLLM(ctx context.Context, t *turn, cited []kb.Hit) bool {
	var srcs []llm.Source
	for i, h := range cited {
		srcs = append(srcs, llm.Source{N: i + 1, Title: h.Chunk.Title + " — " + h.Chunk.Heading, Text: h.Chunk.Text})
	}
	team, mentor := "", ""
	if tp, ok := a.cat.Teams[t.u.TeamID]; ok {
		team = tp.Name
	}
	if m := a.mentorOf(t.u); m != nil {
		mentor = m.Name
	}
	day := clock.DayOfInternship(t.u.StartDate, t.now)
	out, err := a.llm.Compose(ctx, llm.ComposeRequest{Question: t.text, UserName: t.u.Name, TeamName: team, MentorName: mentor, Day: day, Sources: srcs})
	switch {
	case err == nil:
		t.p.Mode = ModeLLM
		t.say(out)
		return true
	case errors.Is(err, llm.ErrNoAnswer):
		t.p.Mode = ModeLLM
		return false
	default:
		a.log.Warn("llm compose failed, offline answer", "err", err)
		t.p.Mode = ModeLLMFallback
		return false
	}
}

func (a *Agent) composeOffline(t *turn, cited []kb.Hit) {
	top := cited[0]
	if top.Chunk.Kind == kb.KindMentor {
		t.say(fmt.Sprintf("%s уже отвечал на похожий вопрос:\n\n%s [1]", top.Chunk.Author, textutil.Truncate(top.Chunk.Text, 700)))
	} else {
		head := top.Chunk.Heading
		if head == top.Chunk.Title {
			head = ""
		}
		intro := "Вот что есть в материалах «" + top.Chunk.Title + "»"
		if head != "" {
			intro += ", раздел «" + head + "»"
		}
		t.say(intro + ":\n\n" + textutil.Truncate(top.Chunk.Text, 700) + " [1]")
	}
	if len(cited) > 1 {
		var more []string
		for i, h := range cited[1:] {
			more = append(more, fmt.Sprintf("«%s» [%d]", h.Chunk.Title, i+2))
		}
		t.say("Ещё по теме: " + strings.Join(more, ", ") + ".")
	}
}

func (a *Agent) noAnswer(ctx context.Context, t *turn) {
	t.p.Intent = "question.no_answer"
	if t.u.Role != directory.RoleIntern || !a.pol.CanAskMentor(t.u) || !a.flags.Enabled(ctx, flags.MentorDigest, t.u.TeamID) {
		t.say("Не нашёл ответа в материалах, доступных тебе. Попробуй переформулировать или спроси коллег в канале команды.")
		return
	}
	t.say("Не нашёл ответа в материалах, доступных тебе — похоже, это вопрос для человека. Не стесняйся: несрочные вопросы наставник разбирает пачкой в сводке, а его ответ я запомню для следующих стажёров.")
	a.planQuestion(ctx, t, t.text)
}
