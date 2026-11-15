package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
)

// CallContext carries the delegated identity of the end user.
type CallContext struct {
	OnBehalfOf string
	RequestID  string
}

// Handler executes a tool.
type Handler func(ctx context.Context, cc CallContext, args json.RawMessage) (any, error)

// Tool is a registered tool.
type Tool struct {
	Info    ToolInfo
	Handler Handler
}

// Server is a minimal MCP server over HTTP.
type Server struct {
	Name, Version string
	Token         string
	tools         map[string]Tool
	// Before runs before each tools/call (latency/fault injection in mocks).
	Before func(ctx context.Context) error
}

// NewServer creates a server; token protects it (service-to-service auth).
func NewServer(name, version, token string) *Server {
	return &Server{Name: name, Version: version, Token: token, tools: map[string]Tool{}}
}

// Register adds a tool.
func (s *Server) Register(t Tool) { s.tools[t.Info.Name] = t }

// Tools lists registered tools sorted by name.
func (s *Server) Tools() []ToolInfo {
	out := make([]ToolInfo, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, t.Info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ErrUnavailable can be returned by Before to simulate an outage (HTTP 503).
var ErrUnavailable = errors.New("service unavailable")

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.Token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.Token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	var req Request
	if err := json.Unmarshal(body, &req); err != nil || req.JSONRPC != "2.0" || req.Method == "" {
		writeJSON(w, http.StatusOK, Response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &RPCError{Code: CodeParse, Message: "invalid JSON-RPC request"}})
		return
	}
	if len(req.ID) == 0 { // notification
		w.WriteHeader(http.StatusAccepted)
		return
	}
	cc := CallContext{OnBehalfOf: r.Header.Get("X-On-Behalf-Of"), RequestID: r.Header.Get("X-Request-Id")}
	var result any
	var rpcErr error
	if s.Before != nil {
		if err := s.Before(r.Context()); err != nil {
			rpcErr = err
			if errors.Is(err, ErrUnavailable) {
				rpcErr = errUnavailable
			}
		}
	}
	if rpcErr == nil {
		result, rpcErr = s.dispatch(r.Context(), cc, req)
	}
	if errors.Is(rpcErr, errUnavailable) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	resp := Response{JSONRPC: "2.0", ID: req.ID}
	if rpcErr != nil {
		var re *RPCError
		if errors.As(rpcErr, &re) {
			resp.Error = re
		} else {
			resp.Error = &RPCError{Code: CodeInternal, Message: rpcErr.Error()}
		}
	} else {
		resp.Result, _ = json.Marshal(result)
	}
	writeJSON(w, http.StatusOK, resp)
}

var errUnavailable = errors.New("unavailable")

func (s *Server) dispatch(ctx context.Context, cc CallContext, req Request) (any, error) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.Tools()}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "invalid params"}
		}
		t, ok := s.tools[p.Name]
		if !ok {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "unknown tool: " + p.Name}
		}
		if len(p.Arguments) == 0 {
			p.Arguments = json.RawMessage("{}")
		}
		out, err := t.Handler(ctx, cc, p.Arguments)
		if err != nil {
			var te *ToolError
			if errors.As(err, &te) {
				sc, _ := json.Marshal(te)
				return CallResult{Content: []Content{{Type: "text", Text: te.Message}}, StructuredContent: sc, IsError: true}, nil
			}
			return nil, &RPCError{Code: CodeInternal, Message: err.Error()}
		}
		sc, _ := json.Marshal(out)
		return CallResult{Content: []Content{{Type: "text", Text: string(sc)}}, StructuredContent: sc}, nil
	}
	return nil, &RPCError{Code: CodeMethodNotFound, Message: "method not found: " + req.Method}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
