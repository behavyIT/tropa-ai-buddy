// Package domain holds plain data types shared across packages.
package domain

import (
	"encoding/json"
	"time"
)

// Task statuses. "blocked" is computed (unfinished dependencies), never stored.
const (
	TaskTodo       = "todo"
	TaskInProgress = "in_progress"
	TaskWaiting    = "waiting"
	TaskBlocked    = "blocked"
	TaskDone       = "done"
)

// TaskState is the persisted status of a route task for a user.
type TaskState struct {
	UserID    string     `json:"user_id"`
	TaskID    string     `json:"task_id"`
	Status    string     `json:"status"`
	UpdatedAt time.Time  `json:"updated_at"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
}

// Access statuses mirrored from the access service.
const (
	AccessGranted  = "granted"
	AccessPending  = "pending_approval"
	AccessRejected = "rejected"
)

// AccessState mirrors a user's access to a system.
type AccessState struct {
	UserID    string    `json:"user_id"`
	System    string    `json:"system"`
	Status    string    `json:"status"`
	RequestID string    `json:"request_id"`
	ActionID  string    `json:"action_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Action types.
const (
	ActionAbsence   = "absence.create"
	ActionAccess    = "access.request"
	ActionChecklist = "checklist.complete"
	ActionQuestion  = "mentor.question"
)

// Action lifecycle statuses (plan → confirm → execute → receipt).
const (
	ActionPlanned   = "planned"
	ActionExecuting = "executing"
	ActionDone      = "done"
	ActionFailed    = "failed"
	ActionCancelled = "cancelled"
)

// External statuses reported by corporate services.
const (
	ExtPendingApproval = "pending_approval"
	ExtApproved        = "approved"
	ExtRejected        = "rejected"
	ExtGranted         = "granted"
	ExtPartial         = "partially_granted"
	ExtCompleted       = "completed"
	ExtOpen            = "open"
	ExtAnswered        = "answered"
)

// Field is a label/value line on an action card.
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ActionView is the human-readable plan/receipt shown before and after execution.
type ActionView struct {
	Service      string   `json:"service"`
	Fields       []Field  `json:"fields"`
	Warnings     []string `json:"warnings,omitempty"`
	ConfirmLabel string   `json:"confirm_label"`
	Approver     string   `json:"approver,omitempty"`
	Permission   string   `json:"permission"`
	Note         string   `json:"note,omitempty"`
}

// Action is a side-effecting operation the buddy prepares and the user confirms.
type Action struct {
	ID             string          `json:"id"`
	UserID         string          `json:"user_id"`
	Type           string          `json:"type"`
	Status         string          `json:"status"`
	Title          string          `json:"title"`
	Args           json.RawMessage `json:"-"`
	View           ActionView      `json:"view"`
	Result         json.RawMessage `json:"result,omitempty"`
	ExternalID     string          `json:"external_id,omitempty"`
	ExternalStatus string          `json:"external_status,omitempty"`
	Error          string          `json:"error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// Citation references a knowledge source used in an answer.
type Citation struct {
	N       int    `json:"n"`
	DocID   string `json:"doc_id"`
	Title   string `json:"title"`
	Kind    string `json:"kind"` // test_instruction | mentor_answer
	Snippet string `json:"snippet"`
	Author  string `json:"author,omitempty"`
	Date    string `json:"date,omitempty"`
}

// PersonCard is a "who to ask" card.
type PersonCard struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Title   string `json:"title"`
	Contact string `json:"contact"`
	Topic   string `json:"topic"`
	How     string `json:"how"`
	Channel string `json:"channel,omitempty"`
}

// StepItem is a route step inside a chat card.
type StepItem struct {
	TaskID string `json:"task_id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Why    string `json:"why"`
	Due    string `json:"due"`
}

// ConceptResult is a graded rubric concept.
type ConceptResult struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
}

// TeachbackResult is the outcome of a "Расскажи о …" check.
type TeachbackResult struct {
	Topic   string          `json:"topic"`
	Title   string          `json:"title"`
	Score   int             `json:"score"` // percent
	Passed  bool            `json:"passed"`
	Covered []ConceptResult `json:"covered"`
	Missing []ConceptResult `json:"missing"`
	DocID   string          `json:"doc_id"`
}

// Card is a rich block attached to a buddy message.
type Card struct {
	Type      string           `json:"type"` // action | person | steps | teachback | requests
	ActionID  string           `json:"action_id,omitempty"`
	Person    *PersonCard      `json:"person,omitempty"`
	Title     string           `json:"title,omitempty"`
	Steps     []StepItem       `json:"steps,omitempty"`
	Teachback *TeachbackResult `json:"teachback,omitempty"`
	ActionIDs []string         `json:"action_ids,omitempty"`
}

// MessagePayload carries structured parts of a chat message.
type MessagePayload struct {
	Intent      string     `json:"intent,omitempty"`
	Mode        string     `json:"mode,omitempty"` // offline | llm | llm_fallback
	Citations   []Citation `json:"citations,omitempty"`
	Cards       []Card     `json:"cards,omitempty"`
	Suggestions []string   `json:"suggestions,omitempty"`
}

// Message is a chat message (private to its owner).
type Message struct {
	ID        string         `json:"id"`
	UserID    string         `json:"-"`
	Role      string         `json:"role"` // user | buddy
	Text      string         `json:"text"`
	Payload   MessagePayload `json:"payload"`
	Feedback  int            `json:"feedback"`
	CreatedAt time.Time      `json:"created_at"`
}

// Question is a question escalated to a mentor.
type Question struct {
	ID         string     `json:"id"`
	AskerID    string     `json:"asker_id"`
	MentorID   string     `json:"mentor_id"`
	TeamID     string     `json:"team_id"`
	Text       string     `json:"text"`
	Context    string     `json:"context"`
	Urgent     bool       `json:"urgent"`
	Status     string     `json:"status"`
	Answer     string     `json:"answer,omitempty"`
	AnsweredBy string     `json:"answered_by,omitempty"`
	AnsweredAt *time.Time `json:"answered_at,omitempty"`
	CardID     string     `json:"card_id,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	Askers     []string   `json:"askers"`
}

// KnowledgeCard is a mentor answer published for future interns.
type KnowledgeCard struct {
	ID               string    `json:"id"`
	TeamID           string    `json:"team_id"`
	Title            string    `json:"title"`
	Body             string    `json:"body"`
	AuthorID         string    `json:"author_id"`
	SourceQuestionID string    `json:"source_question_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// Notification is an in-app notification.
type Notification struct {
	ID        string     `json:"id"`
	UserID    string     `json:"-"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Ref       string     `json:"ref,omitempty"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Event is an analytics event (clickstream-compatible shape).
type Event struct {
	UserID    string         `json:"user_id"`
	TeamID    string         `json:"team_id"`
	Role      string         `json:"role"`
	Name      string         `json:"name"`
	Props     map[string]any `json:"props"`
	CreatedAt time.Time      `json:"created_at"`
}

// Flag is a runtime feature flag value for a scope ("global" or "team:<id>").
type Flag struct {
	Key       string    `json:"key"`
	Scope     string    `json:"scope"`
	Enabled   bool      `json:"enabled"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}
