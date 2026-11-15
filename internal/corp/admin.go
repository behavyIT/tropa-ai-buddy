package corp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Admin controls the corporate mock (demo only): reset and outage simulation.
type Admin interface {
	Reset(ctx context.Context) error
	SetFault(ctx context.Context, on bool) error
}

// HTTPAdmin talks to a remote corp-mcp /admin API.
type HTTPAdmin struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewHTTPAdmin derives the admin base URL from the MCP URL (…/mcp → …).
func NewHTTPAdmin(mcpURL, token string) *HTTPAdmin {
	return &HTTPAdmin{BaseURL: strings.TrimSuffix(mcpURL, "/mcp"), Token: token, HTTP: &http.Client{Timeout: 5 * time.Second}}
}

func (a *HTTPAdmin) post(ctx context.Context, path string, body any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("corp admin %s: http %d", path, resp.StatusCode)
	}
	return nil
}

// Reset clears the mock state.
func (a *HTTPAdmin) Reset(ctx context.Context) error {
	return a.post(ctx, "/admin/reset", map[string]any{})
}

// SetFault toggles a simulated outage.
func (a *HTTPAdmin) SetFault(ctx context.Context, on bool) error {
	return a.post(ctx, "/admin/fault", map[string]bool{"enabled": on})
}
