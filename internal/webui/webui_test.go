package webui_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"tropa/internal/webui"
)

func TestWEB01_SPAFallback(t *testing.T) {
	h := webui.Handler()
	for _, p := range []string{"/", "/team/approvals"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: %d", p, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/.gitkeep", nil))
	if rec.Code != 200 {
		t.Fatalf("static: %d", rec.Code)
	}
}
