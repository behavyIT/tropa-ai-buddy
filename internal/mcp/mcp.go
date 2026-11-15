// Package mcp implements the subset of the Model Context Protocol used to talk
// to corporate services: JSON-RPC 2.0 over Streamable HTTP (JSON responses),
// methods initialize, tools/list and tools/call. Corporate systems (absences,
// access, checklist, calendar) are exposed as MCP tools, so in production the
// mocks are swapped for Avito's real MCP connectors without touching the agent.
package mcp

import (
	"encoding/json"
	"fmt"
)

// ProtocolVersion is the MCP revision implemented.
const ProtocolVersion = "2025-06-18"

// Request is a JSON-RPC request or notification.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("mcp rpc error %d: %s", e.Code, e.Message) }

// Response is a JSON-RPC response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Annotations are MCP tool hints; readOnlyHint=false marks side effects, which
// the agent turns into plan → confirm → execute.
type Annotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint"`
	DestructiveHint bool   `json:"destructiveHint"`
	IdempotentHint  bool   `json:"idempotentHint"`
}

// ToolInfo describes a tool in tools/list.
type ToolInfo struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations Annotations    `json:"annotations"`
}

// Content is a content block of a tool result.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CallResult is the result of tools/call.
type CallResult struct {
	Content           []Content       `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError"`
}

// ToolError is a business error reported by a tool (isError=true). It is not
// retried: the request reached the service and was rejected on purpose.
type ToolError struct {
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (e *ToolError) Error() string { return e.Message }

// Errorf builds a ToolError.
func Errorf(code, format string, a ...any) *ToolError {
	return &ToolError{Code: code, Message: fmt.Sprintf(format, a...)}
}

// JSON-RPC error codes.
const (
	CodeParse          = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603
)
