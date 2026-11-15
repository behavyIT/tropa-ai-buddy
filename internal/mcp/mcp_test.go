package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tropa/internal/mcp"
)

func server(t *testing.T) *mcp.Server {
	s := mcp.NewServer("test", "1", "tok")
	s.Register(mcp.Tool{Info: mcp.ToolInfo{Name: "echo", Description: "echo", InputSchema: map[string]any{"type": "object"}, Annotations: mcp.Annotations{ReadOnlyHint: true}},
		Handler: func(_ context.Context, cc mcp.CallContext, args json.RawMessage) (any, error) {
			return map[string]any{"args": json.RawMessage(args), "actor": cc.OnBehalfOf}, nil
		}})
	s.Register(mcp.Tool{Info: mcp.ToolInfo{Name: "write", Description: "write", Annotations: mcp.Annotations{ReadOnlyHint: false}},
		Handler: func(context.Context, mcp.CallContext, json.RawMessage) (any, error) {
			return nil, mcp.Errorf("forbidden", "нельзя")
		}})
	s.Register(mcp.Tool{Info: mcp.ToolInfo{Name: "boom"},
		Handler: func(context.Context, mcp.CallContext, json.RawMessage) (any, error) {
			return nil, errors.New("internal")
		}})
	return s
}

func TestMCP01_ToolsListWithAnnotations(t *testing.T) {
	srv := httptest.NewServer(server(t))
	defer srv.Close()
	c := mcp.NewClient(srv.URL, "tok", time.Second)
	tools, err := c.ListTools(context.Background())
	if err != nil || len(tools) != 3 || tools[1].Name != "echo" || !tools[1].Annotations.ReadOnlyHint {
		t.Fatalf("tools: %+v %v", tools, err)
	}
	var out struct {
		Actor string `json:"actor"`
	}
	if err := c.Call(context.Background(), "u1", "echo", map[string]int{"a": 1}, &out); err != nil || out.Actor != "u1" {
		t.Fatalf("call: %+v %v", out, err)
	}
	if err := c.Call(context.Background(), "u1", "echo", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMCP02_ToolErrorsAreNotRetried(t *testing.T) {
	var calls atomic.Int32
	s := server(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); s.ServeHTTP(w, r) }))
	defer srv.Close()
	c := mcp.NewClient(srv.URL, "tok", time.Second)
	err := c.Call(context.Background(), "", "write", map[string]any{}, nil)
	var te *mcp.ToolError
	if !errors.As(err, &te) || te.Code != "forbidden" || te.Error() != "нельзя" {
		t.Fatalf("tool error: %v", err)
	}
	if calls.Load() != 2 { // initialize + one call
		t.Fatalf("calls: %d", calls.Load())
	}
	var rpc *mcp.RPCError
	if err := c.Call(context.Background(), "", "boom", nil, nil); !errors.As(err, &rpc) || rpc.Code != mcp.CodeInternal {
		t.Fatalf("internal: %v", err)
	}
	if err := c.Call(context.Background(), "", "missing", nil, nil); !errors.As(err, &rpc) || !strings.Contains(rpc.Error(), "unknown tool") {
		t.Fatalf("unknown: %v", err)
	}
}

func TestMCP03_TransientErrorsRetriedThenBreakerOpens(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var calls atomic.Int32
	s := server(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		s.ServeHTTP(w, r)
	}))
	defer srv.Close()
	c := mcp.NewClient(srv.URL, "tok", time.Second)
	c.Backoff, c.Threshold, c.Cooldown = time.Millisecond, 2, time.Hour
	if err := c.Call(context.Background(), "", "echo", nil, nil); err == nil {
		t.Fatal("expected failure")
	}
	if calls.Load() != 3 {
		t.Fatalf("retries: %d", calls.Load())
	}
	_ = c.Call(context.Background(), "", "echo", nil, nil) // second failure opens the breaker
	before := calls.Load()
	if err := c.Call(context.Background(), "", "echo", nil, nil); !errors.Is(err, mcp.ErrCircuitOpen) {
		t.Fatalf("breaker: %v", err)
	}
	if calls.Load() != before {
		t.Fatal("open breaker must not hit the server")
	}
	fail.Store(false)
	c.Reset()
	if err := c.Call(context.Background(), "", "echo", nil, nil); err != nil {
		t.Fatalf("after reset: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fail.Store(true)
	c.Reset()
	if err := c.Call(ctx, "", "echo", nil, nil); err == nil {
		t.Fatal("cancelled ctx")
	}
}

func TestMCP04_ServerProtocolEdges(t *testing.T) {
	s := server(t)
	s.Before = func(context.Context) error { return nil }
	h := func(method, auth, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/", strings.NewReader(body))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}
	if r := h("GET", "Bearer tok", ""); r.Code != 405 {
		t.Fatalf("GET: %d", r.Code)
	}
	if r := h("POST", "Bearer bad", "{}"); r.Code != 401 {
		t.Fatalf("auth: %d", r.Code)
	}
	if r := h("POST", "Bearer tok", "not json"); !strings.Contains(r.Body.String(), "-32700") {
		t.Fatalf("parse: %s", r.Body.String())
	}
	if r := h("POST", "Bearer tok", `{"jsonrpc":"2.0","method":"notifications/initialized"}`); r.Code != 202 {
		t.Fatalf("notification: %d", r.Code)
	}
	if r := h("POST", "Bearer tok", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); !strings.Contains(r.Body.String(), `"result":{}`) {
		t.Fatalf("ping: %s", r.Body.String())
	}
	if r := h("POST", "Bearer tok", `{"jsonrpc":"2.0","id":1,"method":"nope"}`); !strings.Contains(r.Body.String(), "-32601") {
		t.Fatalf("method: %s", r.Body.String())
	}
	if r := h("POST", "Bearer tok", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"bad"}`); !strings.Contains(r.Body.String(), "-32602") {
		t.Fatalf("params: %s", r.Body.String())
	}
	s.Before = func(context.Context) error { return errors.New("hook") }
	if r := h("POST", "Bearer tok", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); !strings.Contains(r.Body.String(), "hook") {
		t.Fatalf("before hook: %s", r.Body.String())
	}
	// client-side: non-200 that is not transient, and broken JSON
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }))
	defer bad.Close()
	if err := mcp.NewClient(bad.URL, "", time.Second).Call(context.Background(), "", "x", nil, nil); err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("400: %v", err)
	}
	junk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("junk")) }))
	defer junk.Close()
	if err := mcp.NewClient(junk.URL, "", time.Second).Call(context.Background(), "", "x", nil, nil); err == nil {
		t.Fatal("junk")
	}
	text := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcp.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		res := map[string]any{"content": []map[string]string{{"type": "text", "text": `{"v":7}`}}}
		if req.Method == "tools/call" && strings.Contains(string(req.Params), "err") {
			res = map[string]any{"content": []map[string]string{{"type": "text", "text": "плохо"}}, "isError": true}
		}
		b, _ := json.Marshal(res)
		_ = json.NewEncoder(w).Encode(mcp.Response{JSONRPC: "2.0", ID: req.ID, Result: b})
	}))
	defer text.Close()
	var v struct{ V int }
	tc := mcp.NewClient(text.URL, "", time.Second)
	if err := tc.Call(context.Background(), "", "ok", nil, &v); err != nil || v.V != 7 {
		t.Fatalf("text fallback: %v %+v", err, v)
	}
	if err := tc.Call(context.Background(), "", "err", nil, nil); err == nil || err.Error() != "плохо" {
		t.Fatalf("text error: %v", err)
	}
}

func TestMCP09_HandlerTransport(t *testing.T) {
	c := mcp.NewClient("http://in-process/mcp", "tok", time.Second)
	c.HTTP.Transport = mcp.HandlerTransport{Handler: server(t)}
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
}
