package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"tropa/internal/store/storetest"
)

func TestAPP01_BinaryStartsAndStops(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:18392")
	t.Setenv("DATABASE_URL", storetest.DSN())
	t.Setenv("DB_SCHEMA", "")
	t.Setenv("DISABLE_CRON", "true")
	st := storetest.New(t) // ensures the database is reachable (skips otherwise)
	_ = st
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get("http://127.0.0.1:18392/healthz")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tropa did not start")
		}
		time.Sleep(25 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	t.Setenv("SIM_START", "nope")
	if err := run(context.Background()); err == nil {
		t.Fatal("bad config must fail")
	}
}
