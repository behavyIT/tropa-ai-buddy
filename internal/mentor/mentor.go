// Package mentor implements "ask without awkwardness": questions escalated to
// the mentor are de-duplicated and batched into a digest, and a mentor's answer
// can be published as a knowledge card — answered once, reused forever.
package mentor

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"tropa/internal/apperr"
	"tropa/internal/clock"
	"tropa/internal/directory"
	"tropa/internal/domain"
	"tropa/internal/kb"
	"tropa/internal/notify"
	"tropa/internal/policy"
	"tropa/internal/store"
	"tropa/internal/textutil"
)

// SimilarityThreshold for merging an incoming question into an open one.
const SimilarityThreshold = 0.5

// Service handles mentor questions and knowledge cards.
type Service struct {
	st    *store.Store
	clk   clock.Clock
	dir   *directory.Directory
	pol   *policy.Policy
	notif *notify.Notifier
	kb    *kb.Index
}

// New creates the service.
func New(st *store.Store, clk clock.Clock, dir *directory.Directory, pol *policy.Policy, n *notify.Notifier, ix *kb.Index) *Service {
	return &Service{st: st, clk: clk, dir: dir, pol: pol, notif: n, kb: ix}
}

// Ask escalates a question to the asker's mentor. merged=true when an open,
// similar question existed and the asker was subscribed to it instead.
func (s *Service) Ask(ctx context.Context, asker *directory.Person, text, context string, urgent bool) (domain.Question, bool, error) {
	if !s.pol.CanAskMentor(asker) {
		return domain.Question{}, false, apperr.Forbidden("у вас нет назначенного наставника")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return domain.Question{}, false, apperr.BadRequest("пустой вопрос")
	}
	now := s.clk.Now()
	open, err := s.st.QuestionsForMentor(ctx, asker.MentorID)
	if err != nil {
		return domain.Question{}, false, err
	}
	terms := textutil.Terms(text)
	for _, q := range open {
		if q.Status == domain.ExtOpen && textutil.Jaccard(terms, textutil.Terms(q.Text)) >= SimilarityThreshold {
			if err := s.st.AddAsker(ctx, q.ID, asker.ID, urgent, now); err != nil {
				return domain.Question{}, false, err
			}
			s.notif.Track(ctx, asker, "question_escalated", map[string]any{"merged": true, "urgent": urgent})
			q, err = s.st.GetQuestion(ctx, q.ID)
			return q, true, err
		}
	}
	q := domain.Question{ID: uuid.NewString(), AskerID: asker.ID, MentorID: asker.MentorID, TeamID: asker.TeamID, Text: text, Context: context,
		Urgent: urgent, Status: domain.ExtOpen, CreatedAt: now}
	if err := s.st.InsertQuestion(ctx, q); err != nil {
		return domain.Question{}, false, err
	}
	if urgent {
		s.notif.Notify(ctx, asker.MentorID, "question", "Срочный вопрос от "+asker.Name, textutil.Truncate(text, 160), "question:"+q.ID)
	}
	s.notif.Track(ctx, asker, "question_escalated", map[string]any{"merged": false, "urgent": urgent})
	q.Askers = []string{asker.ID}
	return q, false, nil
}

// View is a question with display names for the mentor inbox.
type View struct {
	domain.Question
	AskerNames []string `json:"asker_names"`
	AskCount   int      `json:"ask_count"`
}

// Inbox returns questions addressed to the mentor (or the lead's team).
func (s *Service) Inbox(ctx context.Context, viewer *directory.Person) ([]View, error) {
	var qs []domain.Question
	var err error
	switch viewer.Role {
	case directory.RoleMentor:
		qs, err = s.st.QuestionsForMentor(ctx, viewer.ID)
	case directory.RoleLead:
		qs, err = s.st.QuestionsForTeams(ctx, []string{viewer.TeamID})
	default:
		return nil, apperr.Forbidden("сводка вопросов доступна наставникам и руководителям")
	}
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(qs))
	for _, q := range qs {
		v := View{Question: q, AskCount: len(q.Askers)}
		for _, id := range q.Askers {
			if p, ok := s.dir.ByID(id); ok {
				v.AskerNames = append(v.AskerNames, p.Name)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// Answer records the mentor's answer, notifies every asker in their chat and
// optionally publishes the answer as a team knowledge card.
func (s *Service) Answer(ctx context.Context, mentor *directory.Person, qid, answer string, publish bool) (domain.Question, error) {
	q, err := s.st.GetQuestion(ctx, qid)
	if err == store.ErrNotFound {
		return q, apperr.NotFound("вопрос не найден")
	}
	if err != nil {
		return q, err
	}
	if !s.pol.CanAnswer(mentor, q) {
		return q, apperr.Forbidden("ответить может только наставник, которому адресован вопрос")
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return q, apperr.BadRequest("пустой ответ")
	}
	now := s.clk.Now()
	cardID := ""
	if publish {
		cardID = uuid.NewString()
		if err := s.st.InsertCard(ctx, domain.KnowledgeCard{ID: cardID, TeamID: q.TeamID, Title: q.Text, Body: answer, AuthorID: mentor.ID, SourceQuestionID: q.ID, CreatedAt: now}); err != nil {
			return q, err
		}
		if err := s.RebuildIndex(ctx); err != nil {
			return q, err
		}
	}
	ok, err := s.st.AnswerQuestion(ctx, q.ID, answer, mentor.ID, cardID, now)
	if err != nil {
		return q, err
	}
	if !ok {
		return q, apperr.Conflict("на вопрос уже ответили")
	}
	_ = s.st.SetExternalStatusByExternalID(ctx, domain.ActionQuestion, q.ID, domain.ExtAnswered, now)
	for _, askerID := range q.Askers {
		s.notif.Notify(ctx, askerID, "answer", mentor.Name+" ответил на твой вопрос", textutil.Truncate(answer, 160), "question:"+q.ID)
		msg := domain.Message{ID: uuid.NewString(), UserID: askerID, Role: "buddy", CreatedAt: now,
			Text: fmt.Sprintf("%s ответил на твой вопрос «%s»:\n\n%s", mentor.Name, textutil.Truncate(q.Text, 120), answer),
			Payload: domain.MessagePayload{Intent: "mentor_answer", Mode: "offline", Citations: []domain.Citation{{N: 1, DocID: "question:" + q.ID,
				Title: "Ответ наставника", Kind: kb.KindMentor, Snippet: textutil.Truncate(answer, 200), Author: mentor.Name, Date: now.Format("02.01.2006")}}}}
		_ = s.st.InsertMessage(ctx, msg)
	}
	s.notif.Track(ctx, mentor, "mentor_answered", map[string]any{"published": publish, "askers": len(q.Askers)})
	return s.st.GetQuestion(ctx, q.ID)
}

// RebuildIndex re-indexes the knowledge base with all cards.
func (s *Service) RebuildIndex(ctx context.Context) error {
	cards, err := s.st.ListCards(ctx)
	if err != nil {
		return err
	}
	s.kb.Rebuild(cards)
	return nil
}
