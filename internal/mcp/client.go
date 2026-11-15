package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ErrCircuitOpen is returned while the circuit breaker is open.
var ErrCircuitOpen = errors.New("корпоративный сервис временно недоступен")

// Client is an MCP client with timeouts, retries and a circuit breaker.
// Retries are safe because every side-effecting call carries an idempotency key.
type Client struct {
	URL     string
	Token   string
	HTTP    *http.Client
	Retries int
	Backoff time.Duration

	seq         atomic.Int64
	mu          sync.Mutex
	initialized bool
	failures    int
	openUntil   time.Time
	Threshold   int
	Cooldown    time.Duration
	now         func() time.Time
}

// NewClient creates a client.
func NewClient(url, token string, timeout time.Duration) *Client {
	return &Client{URL: url, Token: token, HTTP: &http.Client{Timeout: timeout}, Retries: 2, Backoff: 150 * time.Millisecond,
		Threshold: 5, Cooldown: 15 * time.Second, now: time.Now}
}

type transientError struct{ err error }

func (e transientError) Error() string { return e.err.Error() }
func (e transientError) Unwrap() error { return e.err }

func (c *Client) breakerAllow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now().After(c.openUntil)
}

func (c *Client) record(ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ok {
		c.failures = 0
		return
	}
	c.failures++
	if c.failures >= c.Threshold {
		c.openUntil = c.now().Add(c.Cooldown)
		c.failures = 0
	}
}

// Reset closes the circuit breaker (used by demo reset).
func (c *Client) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures, c.openUntil, c.initialized = 0, time.Time{}, false
}

func (c *Client) rpc(ctx context.Context, onBehalfOf, method string, params any) (json.RawMessage, error) {
	if !c.breakerAllow() {
		return nil, ErrCircuitOpen
	}
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.Backoff * time.Duration(attempt*attempt)):
			}
		}
		res, err := c.once(ctx, onBehalfOf, method, params)
		if err == nil {
			c.record(true)
			return res, nil
		}
		lastErr = err
		var te transientError
		if !errors.As(err, &te) {
			c.record(true) // service answered: not an availability problem
			return nil, err
		}
	}
	c.record(false)
	return nil, lastErr
}

func (c *Client) once(ctx context.Context, onBehalfOf, method string, params any) (json.RawMessage, error) {
	id := c.seq.Add(1)
	p, _ := json.Marshal(params)
	body, _ := json.Marshal(Request{JSONRPC: "2.0", ID: json.RawMessage(fmt.Sprint(id)), Method: method, Params: p})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if onBehalfOf != "" {
		req.Header.Set("X-On-Behalf-Of", onBehalfOf)
	}
	req.Header.Set("X-Request-Id", fmt.Sprintf("tropa-%d", id))
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, transientError{err}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return nil, transientError{fmt.Errorf("mcp http %d", resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcp http %d", resp.StatusCode)
	}
	var r Response
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("mcp decode: %w", err)
	}
	if r.Error != nil {
		return nil, r.Error
	}
	return r.Result, nil
}

func (c *Client) ensureInit(ctx context.Context) error {
	c.mu.Lock()
	done := c.initialized
	c.mu.Unlock()
	if done {
		return nil
	}
	_, err := c.rpc(ctx, "", "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "tropa-ai-buddy", "version": "1.0.0"},
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

// ListTools returns tools exposed by the server.
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	if err := c.ensureInit(ctx); err != nil {
		return nil, err
	}
	raw, err := c.rpc(ctx, "", "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var r struct {
		Tools []ToolInfo `json:"tools"`
	}
	return r.Tools, json.Unmarshal(raw, &r)
}

// Call invokes a tool on behalf of a user and decodes structuredContent into out.
func (c *Client) Call(ctx context.Context, onBehalfOf, name string, args, out any) error {
	if err := c.ensureInit(ctx); err != nil {
		return err
	}
	raw, err := c.rpc(ctx, onBehalfOf, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return err
	}
	var res CallResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("mcp result: %w", err)
	}
	if res.IsError {
		te := &ToolError{Code: "error"}
		if len(res.StructuredContent) > 0 {
			_ = json.Unmarshal(res.StructuredContent, te)
		}
		if te.Message == "" && len(res.Content) > 0 {
			te.Message = res.Content[0].Text
		}
		return te
	}
	if out == nil {
		return nil
	}
	if len(res.StructuredContent) > 0 {
		return json.Unmarshal(res.StructuredContent, out)
	}
	if len(res.Content) > 0 {
		return json.Unmarshal([]byte(res.Content[0].Text), out)
	}
	return nil
}
