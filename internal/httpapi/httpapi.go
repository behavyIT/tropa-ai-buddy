// Package httpapi exposes the REST API consumed by the React client.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"tropa/internal/apperr"
	"tropa/internal/auth"
	"tropa/internal/directory"
	"tropa/internal/ratelimit"
)

// Metrics are Prometheus collectors (Grafana-ready).
type Metrics struct {
	Requests *prometheus.CounterVec
	Latency  *prometheus.HistogramVec
	Turns    *prometheus.CounterVec
}

// NewMetrics registers collectors in reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tropa_http_requests_total", Help: "HTTP requests"}, []string{"route", "code"}),
		Latency:  prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "tropa_http_request_seconds", Help: "HTTP latency", Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"route"}),
		Turns:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tropa_chat_turns_total", Help: "Chat turns"}, []string{"intent", "mode"}),
	}
	reg.MustRegister(m.Requests, m.Latency, m.Turns)
	return m
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) { w.code = c; w.ResponseWriter.WriteHeader(c) }

// Middleware wraps a handler with recovery, security headers, metrics and logging.
func Middleware(next http.Handler, m *Metrics, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: 200}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic", "err", rec, "stack", string(debug.Stack()))
				writeErr(sw, apperr.Unavailable("внутренняя ошибка"))
			}
			route := r.Pattern
			if route == "" {
				route = "static"
			}
			m.Requests.WithLabelValues(route, http.StatusText(sw.code)).Inc()
			m.Latency.WithLabelValues(route).Observe(time.Since(start).Seconds())
			if strings.HasPrefix(r.URL.Path, "/api/") {
				log.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.code, "ms", time.Since(start).Milliseconds())
			}
		}()
		next.ServeHTTP(sw, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	if e, ok := apperr.As(err); ok {
		writeJSON(w, e.Status, map[string]any{"error": e})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"code": "internal", "message": "Что-то пошло не так. Попробуйте ещё раз."}})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return apperr.BadRequest("некорректный JSON: %v", err)
	}
	return nil
}

type handlerFunc func(w http.ResponseWriter, r *http.Request, u *directory.Person) error

// guard authenticates, enforces CSRF protection on mutations and maps errors.
func (s *Server) guard(fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.auth.FromRequest(r)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "unauthorized", "message": "Нужно войти"}})
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-Requested-With") != "tropa" {
			writeErr(w, apperr.Forbidden("CSRF: отсутствует заголовок X-Requested-With"))
			return
		}
		ctx := auth.WithUser(r.Context(), u)
		if err := fn(w, r.WithContext(ctx), u); err != nil {
			if _, ok := apperr.As(err); !ok && !errors.Is(err, context.Canceled) {
				s.log.Error("handler error", "path", r.URL.Path, "err", err)
			}
			writeErr(w, err)
		}
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) limit(ctx context.Context, key string, n int, d time.Duration) error {
	ok, err := s.limiter.Allow(ctx, key, n, d)
	if err != nil {
		s.log.Warn("rate limiter error (fail-open)", "err", err)
		return nil
	}
	if !ok {
		return apperr.TooMany("Слишком много запросов — подождите немного.")
	}
	return nil
}

var _ ratelimit.Limiter = (*ratelimit.Memory)(nil)
