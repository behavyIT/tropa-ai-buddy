// Package store is the PostgreSQL persistence layer.
package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"tropa/internal/domain"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps a pgx pool.
type Store struct{ pool *pgxpool.Pool }

// Open connects to PostgreSQL. schema, when non-empty, becomes the search_path.
func Open(ctx context.Context, dsn, schema string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if schema != "" {
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
	}
	cfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// Ping checks connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Migrate applies embedded SQL migrations in order.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `create table if not exists schema_migrations (version text primary key, applied_at timestamptz not null default now())`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		var exists bool
		if err := s.pool.QueryRow(ctx, `select exists(select 1 from schema_migrations where version=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sqlText, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sqlText)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `insert into schema_migrations(version) values ($1)`, name)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

// Truncate wipes all application data (demo reset).
func (s *Store) Truncate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `truncate sim_clock, task_states, user_access, actions, messages, chat_state, questions, question_askers, knowledge_cards, notifications, events, feature_flags, cron_runs`)
	return err
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return "{}"
	}
	return string(b)
}

// ---- simulated clock ----

// LoadClock returns the persisted simulated clock.
func (s *Store) LoadClock(ctx context.Context) (base, setAt time.Time, ok bool, err error) {
	err = s.pool.QueryRow(ctx, `select sim_base, set_at from sim_clock where id=1`).Scan(&base, &setAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return base, setAt, false, nil
	}
	return base, setAt, err == nil, err
}

// SaveClock persists the simulated clock.
func (s *Store) SaveClock(ctx context.Context, base, setAt time.Time) error {
	_, err := s.pool.Exec(ctx, `insert into sim_clock(id, sim_base, set_at) values (1,$1,$2)
		on conflict (id) do update set sim_base=excluded.sim_base, set_at=excluded.set_at`, base, setAt)
	return err
}

// ---- route task states ----

// TaskStates returns persisted task states of a user keyed by task id.
func (s *Store) TaskStates(ctx context.Context, userID string) (map[string]domain.TaskState, error) {
	rows, err := s.pool.Query(ctx, `select user_id, task_id, status, updated_at, done_at from task_states where user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.TaskState{}
	for rows.Next() {
		var t domain.TaskState
		if err := rows.Scan(&t.UserID, &t.TaskID, &t.Status, &t.UpdatedAt, &t.DoneAt); err != nil {
			return nil, err
		}
		out[t.TaskID] = t
	}
	return out, rows.Err()
}

// SetTaskStatus upserts a task status.
func (s *Store) SetTaskStatus(ctx context.Context, userID, taskID, status string, now time.Time) error {
	var doneAt *time.Time
	if status == domain.TaskDone {
		doneAt = &now
	}
	_, err := s.pool.Exec(ctx, `insert into task_states(user_id, task_id, status, updated_at, done_at) values ($1,$2,$3,$4,$5)
		on conflict (user_id, task_id) do update set status=excluded.status, updated_at=excluded.updated_at,
		done_at=coalesce(excluded.done_at, case when excluded.status='done' then task_states.done_at else null end)`,
		userID, taskID, status, now, doneAt)
	return err
}

// ---- access mirror ----

// AccessStates returns a user's mirrored access states keyed by system.
func (s *Store) AccessStates(ctx context.Context, userID string) (map[string]domain.AccessState, error) {
	rows, err := s.pool.Query(ctx, `select user_id, system, status, request_id, action_id, updated_at from user_access where user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.AccessState{}
	for rows.Next() {
		var a domain.AccessState
		if err := rows.Scan(&a.UserID, &a.System, &a.Status, &a.RequestID, &a.ActionID, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out[a.System] = a
	}
	return out, rows.Err()
}

// UpsertAccess stores an access state.
func (s *Store) UpsertAccess(ctx context.Context, a domain.AccessState) error {
	_, err := s.pool.Exec(ctx, `insert into user_access(user_id, system, status, request_id, action_id, updated_at) values ($1,$2,$3,$4,$5,$6)
		on conflict (user_id, system) do update set status=excluded.status, request_id=excluded.request_id, action_id=excluded.action_id, updated_at=excluded.updated_at`,
		a.UserID, a.System, a.Status, a.RequestID, a.ActionID, a.UpdatedAt)
	return err
}

// ---- actions ----

const actionCols = `id, user_id, type, status, title, args, view, coalesce(result, 'null'::jsonb), external_id, external_status, error, created_at, updated_at`

func scanAction(row pgx.Row) (domain.Action, error) {
	var a domain.Action
	var args, view, result []byte
	err := row.Scan(&a.ID, &a.UserID, &a.Type, &a.Status, &a.Title, &args, &view, &result, &a.ExternalID, &a.ExternalStatus, &a.Error, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.Args = args
	if string(result) != "null" {
		a.Result = result
	}
	_ = json.Unmarshal(view, &a.View)
	return a, nil
}

// InsertAction stores a new action.
func (s *Store) InsertAction(ctx context.Context, a domain.Action) error {
	args := string(a.Args)
	if args == "" {
		args = "{}"
	}
	_, err := s.pool.Exec(ctx, `insert into actions(id, user_id, type, status, title, args, view, external_id, external_status, error, created_at, updated_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		a.ID, a.UserID, a.Type, a.Status, a.Title, args, toJSON(a.View), a.ExternalID, a.ExternalStatus, a.Error, a.CreatedAt, a.UpdatedAt)
	return err
}

// GetAction loads an action.
func (s *Store) GetAction(ctx context.Context, id string) (domain.Action, error) {
	return scanAction(s.pool.QueryRow(ctx, `select `+actionCols+` from actions where id=$1`, id))
}

func (s *Store) queryActions(ctx context.Context, q string, args ...any) ([]domain.Action, error) {
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Action
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListActions returns a user's latest actions.
func (s *Store) ListActions(ctx context.Context, userID string, limit int) ([]domain.Action, error) {
	return s.queryActions(ctx, `select `+actionCols+` from actions where user_id=$1 and status <> 'cancelled' order by created_at desc limit $2`, userID, limit)
}

// ListActionsForUsers returns actions of given users/types with an external status.
func (s *Store) ListActionsForUsers(ctx context.Context, userIDs, types []string, extStatus string) ([]domain.Action, error) {
	return s.queryActions(ctx, `select `+actionCols+` from actions where user_id = any($1) and type = any($2) and status='done' and external_status=$3 order by created_at`, userIDs, types, extStatus)
}

// PendingExternal returns executed actions still awaiting an external decision.
func (s *Store) PendingExternal(ctx context.Context) ([]domain.Action, error) {
	return s.queryActions(ctx, `select `+actionCols+` from actions where status='done' and external_status in ('pending_approval','partially_granted') order by created_at`)
}

// UpdatePlan replaces args/view/title of a still-planned action.
func (s *Store) UpdatePlan(ctx context.Context, id string, title string, args []byte, view domain.ActionView, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `update actions set title=$2, args=$3, view=$4, updated_at=$5 where id=$1 and status='planned'`, id, title, string(args), toJSON(view), now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TransitionAction atomically moves an action from one status to another.
// It returns false when the action was not in the expected status — this is
// what makes "confirm" exactly-once even under double clicks or retries.
func (s *Store) TransitionAction(ctx context.Context, id, from, to string, now time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update actions set status=$3, updated_at=$4 where id=$1 and status=$2`, id, from, to, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// FinishAction stores the execution outcome.
func (s *Store) FinishAction(ctx context.Context, id, status string, result any, externalID, externalStatus, errText string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `update actions set status=$2, result=$3, external_id=$4, external_status=$5, error=$6, updated_at=$7 where id=$1`,
		id, status, toJSON(result), externalID, externalStatus, errText, now)
	return err
}

// SetExternalStatus updates the external status (and optionally the result) of an action.
func (s *Store) SetExternalStatus(ctx context.Context, id, extStatus string, result any, now time.Time) error {
	if result == nil {
		_, err := s.pool.Exec(ctx, `update actions set external_status=$2, updated_at=$3 where id=$1`, id, extStatus, now)
		return err
	}
	_, err := s.pool.Exec(ctx, `update actions set external_status=$2, result=$3, updated_at=$4 where id=$1`, id, extStatus, toJSON(result), now)
	return err
}

// ---- messages ----

// InsertMessage stores a chat message.
func (s *Store) InsertMessage(ctx context.Context, m domain.Message) error {
	_, err := s.pool.Exec(ctx, `insert into messages(id, user_id, role, text, payload, created_at) values ($1,$2,$3,$4,$5,$6)`,
		m.ID, m.UserID, m.Role, m.Text, toJSON(m.Payload), m.CreatedAt)
	return err
}

// ListMessages returns the latest messages of a user in chronological order.
func (s *Store) ListMessages(ctx context.Context, userID string, limit int) ([]domain.Message, error) {
	rows, err := s.pool.Query(ctx, `select id, user_id, role, text, payload, feedback, created_at from (
		select * from messages where user_id=$1 order by created_at desc limit $2) t order by created_at`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		var m domain.Message
		var payload []byte
		if err := rows.Scan(&m.ID, &m.UserID, &m.Role, &m.Text, &payload, &m.Feedback, &m.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(payload, &m.Payload)
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetFeedback stores 👍(1)/👎(-1) for a buddy message owned by userID.
func (s *Store) SetFeedback(ctx context.Context, id, userID string, v int) (domain.Message, error) {
	var m domain.Message
	var payload []byte
	err := s.pool.QueryRow(ctx, `update messages set feedback=$3 where id=$1 and user_id=$2 and role='buddy'
		returning id, user_id, role, text, payload, feedback, created_at`, id, userID, v).
		Scan(&m.ID, &m.UserID, &m.Role, &m.Text, &payload, &m.Feedback, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	_ = json.Unmarshal(payload, &m.Payload)
	return m, err
}

// ---- chat state ----

// GetChatState returns the conversation state of a user (empty map if none).
func (s *Store) GetChatState(ctx context.Context, userID string) (map[string]string, error) {
	var b []byte
	err := s.pool.QueryRow(ctx, `select state from chat_state where user_id=$1`, userID).Scan(&b)
	out := map[string]string{}
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(b, &out)
	return out, nil
}

// SetChatState replaces the conversation state.
func (s *Store) SetChatState(ctx context.Context, userID string, st map[string]string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `insert into chat_state(user_id, state, updated_at) values ($1,$2,$3)
		on conflict (user_id) do update set state=excluded.state, updated_at=excluded.updated_at`, userID, toJSON(st), now)
	return err
}

// ---- questions & knowledge cards ----

// InsertQuestion stores a new mentor question with its first asker.
func (s *Store) InsertQuestion(ctx context.Context, q domain.Question) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `insert into questions(id, asker_id, mentor_id, team_id, text, context, urgent, status, created_at)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, q.ID, q.AskerID, q.MentorID, q.TeamID, q.Text, q.Context, q.Urgent, q.Status, q.CreatedAt); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into question_askers(question_id, user_id, created_at) values ($1,$2,$3)`, q.ID, q.AskerID, q.CreatedAt)
		return err
	})
}

// AddAsker subscribes another asker to an existing question (de-duplication).
func (s *Store) AddAsker(ctx context.Context, questionID, userID string, urgent bool, now time.Time) error {
	if _, err := s.pool.Exec(ctx, `insert into question_askers(question_id, user_id, created_at) values ($1,$2,$3) on conflict do nothing`, questionID, userID, now); err != nil {
		return err
	}
	if urgent {
		_, err := s.pool.Exec(ctx, `update questions set urgent=true where id=$1`, questionID)
		return err
	}
	return nil
}

const questionCols = `q.id, q.asker_id, q.mentor_id, q.team_id, q.text, q.context, q.urgent, q.status, q.answer, q.answered_by, q.answered_at, q.card_id, q.created_at,
	coalesce((select array_agg(a.user_id order by a.created_at) from question_askers a where a.question_id=q.id), '{}')`

func (s *Store) queryQuestions(ctx context.Context, where string, args ...any) ([]domain.Question, error) {
	rows, err := s.pool.Query(ctx, `select `+questionCols+` from questions q where `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Question
	for rows.Next() {
		var q domain.Question
		if err := rows.Scan(&q.ID, &q.AskerID, &q.MentorID, &q.TeamID, &q.Text, &q.Context, &q.Urgent, &q.Status, &q.Answer, &q.AnsweredBy, &q.AnsweredAt, &q.CardID, &q.CreatedAt, &q.Askers); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// QuestionsForMentor returns all questions addressed to a mentor (open first).
func (s *Store) QuestionsForMentor(ctx context.Context, mentorID string) ([]domain.Question, error) {
	return s.queryQuestions(ctx, `q.mentor_id=$1 order by (q.status='open') desc, q.urgent desc, q.created_at`, mentorID)
}

// QuestionsForTeams returns questions of given teams.
func (s *Store) QuestionsForTeams(ctx context.Context, teams []string) ([]domain.Question, error) {
	return s.queryQuestions(ctx, `q.team_id = any($1) order by q.created_at`, teams)
}

// GetQuestion loads a question.
func (s *Store) GetQuestion(ctx context.Context, id string) (domain.Question, error) {
	qs, err := s.queryQuestions(ctx, `q.id=$1`, id)
	if err != nil {
		return domain.Question{}, err
	}
	if len(qs) == 0 {
		return domain.Question{}, ErrNotFound
	}
	return qs[0], nil
}

// AnswerQuestion records the mentor's answer; false if it was already answered.
func (s *Store) AnswerQuestion(ctx context.Context, id, answer, by, cardID string, now time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update questions set status='answered', answer=$2, answered_by=$3, card_id=$4, answered_at=$5 where id=$1 and status='open'`, id, answer, by, cardID, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// InsertCard stores a knowledge card.
func (s *Store) InsertCard(ctx context.Context, c domain.KnowledgeCard) error {
	_, err := s.pool.Exec(ctx, `insert into knowledge_cards(id, team_id, title, body, author_id, source_question_id, created_at) values ($1,$2,$3,$4,$5,$6,$7)`,
		c.ID, c.TeamID, c.Title, c.Body, c.AuthorID, c.SourceQuestionID, c.CreatedAt)
	return err
}

// ListCards returns all knowledge cards.
func (s *Store) ListCards(ctx context.Context) ([]domain.KnowledgeCard, error) {
	rows, err := s.pool.Query(ctx, `select id, team_id, title, body, author_id, source_question_id, created_at from knowledge_cards order by created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.KnowledgeCard
	for rows.Next() {
		var c domain.KnowledgeCard
		if err := rows.Scan(&c.ID, &c.TeamID, &c.Title, &c.Body, &c.AuthorID, &c.SourceQuestionID, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- notifications ----

// InsertNotification stores a notification.
func (s *Store) InsertNotification(ctx context.Context, n domain.Notification) error {
	_, err := s.pool.Exec(ctx, `insert into notifications(id, user_id, kind, title, body, ref, created_at) values ($1,$2,$3,$4,$5,$6,$7)`,
		n.ID, n.UserID, n.Kind, n.Title, n.Body, n.Ref, n.CreatedAt)
	return err
}

// ListNotifications returns latest notifications and the unread count.
func (s *Store) ListNotifications(ctx context.Context, userID string, limit int) ([]domain.Notification, int, error) {
	rows, err := s.pool.Query(ctx, `select id, user_id, kind, title, body, ref, read_at, created_at from notifications where user_id=$1 order by created_at desc limit $2`, userID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Notification
	for rows.Next() {
		var n domain.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, &n.Title, &n.Body, &n.Ref, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var unread int
	err = s.pool.QueryRow(ctx, `select count(*) from notifications where user_id=$1 and read_at is null`, userID).Scan(&unread)
	return out, unread, err
}

// MarkAllRead marks notifications as read.
func (s *Store) MarkAllRead(ctx context.Context, userID string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `update notifications set read_at=$2 where user_id=$1 and read_at is null`, userID, now)
	return err
}

// ---- events ----

// InsertEvent stores an analytics event.
func (s *Store) InsertEvent(ctx context.Context, e domain.Event) error {
	_, err := s.pool.Exec(ctx, `insert into events(user_id, team_id, role, name, props, created_at) values ($1,$2,$3,$4,$5,$6)`,
		e.UserID, e.TeamID, e.Role, e.Name, toJSON(e.Props), e.CreatedAt)
	return err
}

// ListEvents returns events of the given teams (all teams when empty).
func (s *Store) ListEvents(ctx context.Context, teams []string) ([]domain.Event, error) {
	q := `select user_id, team_id, role, name, props, created_at from events`
	var args []any
	if len(teams) > 0 {
		q += ` where team_id = any($1)`
		args = append(args, teams)
	}
	rows, err := s.pool.Query(ctx, q+` order by id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var props []byte
		if err := rows.Scan(&e.UserID, &e.TeamID, &e.Role, &e.Name, &props, &e.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(props, &e.Props)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- feature flags ----

// ListFlags returns all stored flags.
func (s *Store) ListFlags(ctx context.Context) ([]domain.Flag, error) {
	rows, err := s.pool.Query(ctx, `select key, scope, enabled, updated_by, updated_at from feature_flags order by key, scope`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Flag
	for rows.Next() {
		var f domain.Flag
		if err := rows.Scan(&f.Key, &f.Scope, &f.Enabled, &f.UpdatedBy, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetFlag upserts a flag value.
func (s *Store) SetFlag(ctx context.Context, f domain.Flag) error {
	_, err := s.pool.Exec(ctx, `insert into feature_flags(key, scope, enabled, updated_by, updated_at) values ($1,$2,$3,$4,$5)
		on conflict (key, scope) do update set enabled=excluded.enabled, updated_by=excluded.updated_by, updated_at=excluded.updated_at`,
		f.Key, f.Scope, f.Enabled, f.UpdatedBy, f.UpdatedAt)
	return err
}

// ---- cron ----

// TryMarkRun records a job run for key; false if it already ran (idempotency).
func (s *Store) TryMarkRun(ctx context.Context, job, key string, now time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `insert into cron_runs(job, key, ran_at) values ($1,$2,$3) on conflict do nothing`, job, key, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// SchemaName sanitizes a schema identifier for tests.
func SchemaName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// CreateSchema creates a schema (used by tests for isolation).
func (s *Store) CreateSchema(ctx context.Context, name string) error {
	_, err := s.pool.Exec(ctx, `create schema if not exists `+pgx.Identifier{name}.Sanitize())
	return err
}

// DropSchema drops a schema (used by tests for isolation).
func (s *Store) DropSchema(ctx context.Context, name string) error {
	_, err := s.pool.Exec(ctx, `drop schema if exists `+pgx.Identifier{name}.Sanitize()+` cascade`)
	return err
}

// SetExternalStatusByExternalID updates actions of a type that reference an external id.
func (s *Store) SetExternalStatusByExternalID(ctx context.Context, typ, extID, extStatus string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `update actions set external_status=$3, updated_at=$4 where type=$1 and external_id=$2 and status='done'`, typ, extID, extStatus, now)
	return err
}
