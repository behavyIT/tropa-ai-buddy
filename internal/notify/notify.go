// Package notify writes in-app notifications and analytics events. Events have
// a clickstream-compatible shape; in production the sink would be Kafka
// (outbox pattern) — here they are stored in PostgreSQL and logged.
package notify

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"tropa/internal/clock"
	"tropa/internal/directory"
	"tropa/internal/domain"
)

// Store is the persistence needed.
type Store interface {
	InsertNotification(ctx context.Context, n domain.Notification) error
	InsertEvent(ctx context.Context, e domain.Event) error
}

// Notifier sends notifications and tracks events.
type Notifier struct {
	st  Store
	clk clock.Clock
	log *slog.Logger
}

// New creates a notifier.
func New(st Store, clk clock.Clock, log *slog.Logger) *Notifier {
	return &Notifier{st: st, clk: clk, log: log}
}

// Notify stores a notification for a user.
func (n *Notifier) Notify(ctx context.Context, userID, kind, title, body, ref string) {
	err := n.st.InsertNotification(ctx, domain.Notification{ID: uuid.NewString(), UserID: userID, Kind: kind, Title: title, Body: body, Ref: ref, CreatedAt: n.clk.Now()})
	if err != nil {
		n.log.Error("notify failed", "err", err, "user", userID)
	}
}

// Track records an analytics event; failures never break the user flow.
func (n *Notifier) Track(ctx context.Context, u *directory.Person, name string, props map[string]any) {
	e := domain.Event{Name: name, Props: props, CreatedAt: n.clk.Now()}
	if u != nil {
		e.UserID, e.TeamID, e.Role = u.ID, u.TeamID, u.Role
	}
	if err := n.st.InsertEvent(ctx, e); err != nil {
		n.log.Error("event failed", "err", err, "event", name)
		return
	}
	n.log.Debug("event", "name", name, "user", e.UserID, "props", props)
}
