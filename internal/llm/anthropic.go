package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// AnthropicConfig configures the Claude provider.
type AnthropicConfig struct {
	APIKey         string
	BaseURL        string
	Model          string // default claude-opus-5-5
	Effort         string // low | medium | high
	ServerFallback bool   // opt into server-side refusal fallbacks
	Timeout        time.Duration
}

// Anthropic implements Provider with the official Anthropic Go SDK.
type Anthropic struct {
	client anthropic.Client
	cfg    AnthropicConfig
}

// NewAnthropic creates the provider.
func NewAnthropic(cfg AnthropicConfig) *Anthropic {
	if cfg.Model == "" {
		cfg.Model = "claude-opus-5-5"
	}
	if cfg.Effort == "" {
		cfg.Effort = "low"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 20 * time.Second
	}
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey), option.WithMaxRetries(1)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.ServerFallback {
		// Server-side refusal fallback: a declined request is re-served by the
		// model Anthropic recommends for that refusal category.
		opts = append(opts, option.WithHeaderAdd("anthropic-beta", "server-side-fallback-2026-07-01"), option.WithJSONSet("fallbacks", "default"))
	}
	return &Anthropic{client: anthropic.NewClient(opts...), cfg: cfg}
}

// Name returns the provider name.
func (a *Anthropic) Name() string { return "anthropic:" + a.cfg.Model }

func (a *Anthropic) call(ctx context.Context, system, user string, maxTokens int64, schema map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.cfg.Model),
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(user))},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffort(a.cfg.Effort),
		},
	}
	if schema != nil {
		params.OutputConfig.Format = anthropic.JSONOutputFormatParam{Schema: schema}
	}
	resp, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("anthropic: %w", err)
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return "", ErrRefused
	}
	var b strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	if resp.StopReason == anthropic.StopReasonMaxTokens {
		return "", fmt.Errorf("anthropic: output truncated")
	}
	return strings.TrimSpace(b.String()), nil
}

// UnderstandSchema is the JSON schema for structured intent extraction.
func UnderstandSchema(intents, systems []string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"intent":     map[string]any{"type": "string", "enum": intents},
			"sub":        map[string]any{"type": "string", "enum": []string{"how", "do", ""}},
			"date_from":  map[string]any{"type": "string", "description": "YYYY-MM-DD или пустая строка"},
			"date_to":    map[string]any{"type": "string", "description": "YYYY-MM-DD или пустая строка"},
			"systems":    map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": systems}},
			"topic":      map[string]any{"type": "string"},
			"standard":   map[string]any{"type": "boolean"},
			"confidence": map[string]any{"type": "number"},
		},
		"required":             []string{"intent", "sub", "date_from", "date_to", "systems", "topic", "standard", "confidence"},
		"additionalProperties": false,
	}
}

// Understand extracts intent and slots with structured outputs.
func (a *Anthropic) Understand(ctx context.Context, r UnderstandRequest) (Understanding, error) {
	system := understandSystem(r)
	out, err := a.call(ctx, system, "<message>\n"+r.Text+"\n</message>", 4096, UnderstandSchema(r.Intents, r.Systems))
	if err != nil {
		return Understanding{}, err
	}
	var u Understanding
	if err := json.Unmarshal([]byte(out), &u); err != nil {
		return u, fmt.Errorf("understand decode: %w", err)
	}
	return u, nil
}

// Compose writes an answer grounded in sources; ErrNoAnswer when sources don't cover it.
func (a *Anthropic) Compose(ctx context.Context, r ComposeRequest) (string, error) {
	out, err := a.call(ctx, composeSystem(r), composeUser(r), 8192, nil)
	if err != nil {
		return "", err
	}
	if strings.Contains(out, "НЕТ_ОТВЕТА") || out == "" {
		return "", ErrNoAnswer
	}
	return out, nil
}

// GradeSchema is the JSON schema for teach-back grading.
func GradeSchema(ids []string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"covered":  map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": ids}},
			"feedback": map[string]any{"type": "string"},
		},
		"required":             []string{"covered", "feedback"},
		"additionalProperties": false,
	}
}

// Grade grades a teach-back explanation against the rubric.
func (a *Anthropic) Grade(ctx context.Context, r GradeRequest) (GradeResult, error) {
	ids := make([]string, len(r.Concepts))
	for i, c := range r.Concepts {
		ids[i] = c.ID
	}
	out, err := a.call(ctx, gradeSystem(r), "<explanation>\n"+r.Explanation+"\n</explanation>", 4096, GradeSchema(ids))
	if err != nil {
		return GradeResult{}, err
	}
	var g GradeResult
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		return g, fmt.Errorf("grade decode: %w", err)
	}
	return g, nil
}
