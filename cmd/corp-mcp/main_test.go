package main

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestMCP06_CorpServerStartsAndStops(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:18391")
	t.Setenv("CORP_STATE_FILE", filepath.Join(t.TempDir(), "state.json"))
	t.Setenv("CORP_LATENCY", "1ms")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := http.Get("http://127.0.0.1:18391/healthz")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("corp-mcp did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	t.Setenv("CORP_LATENCY", "bad")
	if err := run(context.Background()); err == nil {
		t.Fatal("bad latency must fail")
	}
}
