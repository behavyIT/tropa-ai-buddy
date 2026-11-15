// Package cron imitates Avito's internal AI-cron: proactive jobs that make the
// buddy speak first — a morning brief, reminders about overdue steps, automatic
// nudges to mentors when an intern waits on them, a mentor question digest and
// polling of corporate services for decisions. Jobs are idempotent (cron_runs)
// and run on a single leader across replicas (Redis lock).
package cron

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"tropa/internal/actions"
	"tropa/internal/clock"
	"tropa/internal/directory"
	"tropa/internal/domain"
	"tropa/internal/notify"
	"tropa/internal/ratelimit"
	"tropa/internal/route"
	"tropa/internal/store"
)

// Cron runs proactive jobs.
type Cron struct {
	st      *store.Store
	clk     clock.Clock
	dir     *directory.Directory
	rt      *route.Engine
	actions *actions.Service
	notif   *notify.Notifier
	lock    ratelimit.Locker
	log     *slog.Logger
	owner   string
}

// New creates the scheduler.
func New(st *store.Store, clk clock.Clock, dir *directory.Directory, rt *route.Engine, as *actions.Service, n *notify.Notifier, lock ratelimit.Locker, log *slog.Logger) *Cron {
	host, _ := os.Hostname()
	return &Cron{st: st, clk: clk, dir: dir, rt: rt, actions: as, notif: n, lock: lock, log: log, owner: host + "-" + uuid.NewString()[:8]}
}

// Run ticks until ctx is done.
func (c *Cron) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if ok, err := c.lock.TryLock(ctx, "cron-leader", c.owner, every*3); err == nil && ok {
			if err := c.Tick(ctx); err != nil {
				c.log.Warn("cron tick", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick runs all jobs once.
func (c *Cron) Tick(ctx context.Context) error {
	if _, err := c.actions.Sync(ctx); err != nil {
		c.log.Warn("sync external", "err", err)
	}
	now := c.clk.Now()
	if now.Hour() < 9 {
		return nil
	}
	day := now.Format("2006-01-02")
	for _, in := range c.dir.Interns() {
		v, err := c.rt.Build(ctx, in, in, now)
		if err != nil {
			return err
		}
		if err := c.brief(ctx, in, v, day); err != nil {
			return err
		}
		if err := c.overdue(ctx, in, v); err != nil {
			return err
		}
	}
	return c.digest(ctx, day)
}

func (c *Cron) brief(ctx context.Context, in *directory.Person, v route.View, day string) error {
	ok, err := c.st.TryMarkRun(ctx, "brief", in.ID+"|"+day, c.clk.Now())
	if err != nil || !ok {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Доброе утро, %s! День %d стажировки (%s). Пройдено %d из %d.", in.FirstName(), v.Day, v.Today, v.Done, v.Total)
	if v.Overdue > 0 {
		fmt.Fprintf(&b, " Просрочено: %d — начни с них.", v.Overdue)
	}
	if len(v.Waiting) > 0 {
		b.WriteString(" Ждём: " + v.Waiting[0].Title + ".")
	}
	payload := domain.MessagePayload{Intent: "morning_brief", Mode: "offline", Suggestions: []string{"Что мне делать дальше?"}}
	if len(v.Next) > 0 {
		payload.Cards = append(payload.Cards, domain.Card{Type: "steps", Title: "План на сегодня", Steps: v.Next})
	}
	if len(v.Waiting) > 0 && len(v.WhileWaiting) > 0 {
		payload.Cards = append(payload.Cards, domain.Card{Type: "steps", Title: "Пока ждёшь — можно сделать", Steps: v.WhileWaiting})
	}
	if err := c.st.InsertMessage(ctx, domain.Message{ID: uuid.NewString(), UserID: in.ID, Role: "buddy", Text: b.String(), Payload: payload, CreatedAt: c.clk.Now()}); err != nil {
		return err
	}
	next := "Все пункты на сегодня закрыты."
	if len(v.Next) > 0 {
		next = "Главное сегодня: " + v.Next[0].Title
	}
	c.notif.Notify(ctx, in.ID, "brief", fmt.Sprintf("День %d: утренний бриф", v.Day), next, "chat")
	c.notif.Track(ctx, in, "brief_sent", map[string]any{"day": v.Day})
	return nil
}

func (c *Cron) overdue(ctx context.Context, in *directory.Person, v route.View) error {
	for _, st := range v.Stages {
		for _, t := range st.Tasks {
			if !t.Overdue || t.Status == domain.TaskDone {
				continue
			}
			ok, err := c.st.TryMarkRun(ctx, "overdue", in.ID+"|"+t.ID, c.clk.Now())
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			switch t.Owner {
			case "intern":
				if t.Status != domain.TaskBlocked && t.Status != domain.TaskWaiting {
					c.notif.Notify(ctx, in.ID, "reminder", "Просрочен пункт: «"+t.Title+"»", "Срок был "+t.DueDate+". Нужна помощь — спроси Тропу.", "task:"+t.ID)
				}
			default:
				// "без неловкости": the buddy nudges the mentor/lead instead of the intern
				ownerID := in.MentorID
				if t.Owner == "lead" {
					ownerID = in.LeadID
				}
				c.notif.Notify(ctx, ownerID, "nudge", in.Name+" ждёт: «"+t.Title+"»", "Срок был "+t.DueDate+". Отметьте пункт в маршруте стажёра, когда сделаете.", "intern:"+in.ID)
				c.notif.Track(ctx, in, "nudge_sent", map[string]any{"task": t.ID, "auto": true})
			}
		}
	}
	return nil
}

func (c *Cron) digest(ctx context.Context, day string) error {
	for _, p := range c.dir.All() {
		if p.Role != directory.RoleMentor {
			continue
		}
		qs, err := c.st.QuestionsForMentor(ctx, p.ID)
		if err != nil {
			return err
		}
		open := 0
		for _, q := range qs {
			if q.Status == domain.ExtOpen {
				open++
			}
		}
		if open == 0 {
			continue
		}
		ok, err := c.st.TryMarkRun(ctx, "digest", p.ID+"|"+day, c.clk.Now())
		if err != nil {
			return err
		}
		if ok {
			c.notif.Notify(ctx, p.ID, "digest", fmt.Sprintf("Сводка: %d вопрос(ов) от стажёров", open), "Похожие вопросы уже объединены. Ответ можно сразу сохранить в базу знаний.", "questions")
		}
	}
	return nil
}
