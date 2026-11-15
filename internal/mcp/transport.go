package mcp

import (
	"net/http"
	"net/http/httptest"
)

// HandlerTransport is an http.RoundTripper that serves requests with an
// in-process handler. It lets the single-binary mode keep the full MCP wire
// protocol (JSON-RPC serialization, headers, status codes) without a network hop.
type HandlerTransport struct{ Handler http.Handler }

// RoundTrip implements http.RoundTripper.
func (t HandlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.Handler.ServeHTTP(rec, r)
	resp := rec.Result()
	resp.Request = r
	return resp, nil
}
