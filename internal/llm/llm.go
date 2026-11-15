// Package llm is the optional "language layer" of the buddy. The LLM only
// understands and phrases: it extracts intent/slots, writes grounded answers
// from ACL-filtered snippets and grades teach-back explanations. It never
// decides permissions and never executes side effects. Every call has a
// deterministic fallback, so a slow, failing or refusing model degrades the
// answer quality, not the product.
package llm

import (
	"context"
	"errors"
)

// ErrNoAnswer means the model found no answer in the provided sources.
var ErrNoAnswer = errors.New("no answer in sources")

// ErrRefused means the model declined to answer.
var ErrRefused = errors.New("model refused")

// Source is a snippet the model may cite.
type Source struct {
	N     int
	Title string
	Text  string
}

// UnderstandRequest asks for intent and slots.
type UnderstandRequest struct {
	Text      string
	Today     string // YYYY-MM-DD, weekday
	UserRole  string
	TeamName  string
	Systems   []string
	Topics    []string
	Intents   []string
	StateHint string
}

// Understanding is the structured model output.
type Understanding struct {
	Intent     string   `json:"intent"`
	Sub        string   `json:"sub"`
	DateFrom   string   `json:"date_from"`
	DateTo     string   `json:"date_to"`
	Systems    []string `json:"systems"`
	Topic      string   `json:"topic"`
	Standard   bool     `json:"standard"`
	Confidence float64  `json:"confidence"`
}

// ComposeRequest asks for an answer grounded in sources.
type ComposeRequest struct {
	Question   string
	UserName   string
	TeamName   string
	MentorName string
	Day        int
	Sources    []Source
}

// GradeRequest asks to grade a teach-back explanation against a rubric.
type GradeRequest struct {
	Topic       string
	Explanation string
	Concepts    []Concept
}

// Concept is a rubric point.
type Concept struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// GradeResult lists covered concept ids.
type GradeResult struct {
	Covered  []string `json:"covered"`
	Feedback string   `json:"feedback"`
}

// Provider is an LLM backend.
type Provider interface {
	Name() string
	Understand(ctx context.Context, r UnderstandRequest) (Understanding, error)
	Compose(ctx context.Context, r ComposeRequest) (string, error)
	Grade(ctx context.Context, r GradeRequest) (GradeResult, error)
}
