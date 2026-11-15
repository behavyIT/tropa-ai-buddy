// Command corp-mcp runs the imitation of Avito corporate services as an MCP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tropa/content"
	"tropa/internal/catalog"
	"tropa/internal/corpmock"
	"tropa/internal/directory"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "corp-mcp:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cat, err := catalog.Load(content.FS)
	if err != nil {
		return err
	}
	token := getenv("CORP_TOKEN", "dev-corp-token")
	m := corpmock.New(directory.New(), cat, token, nil)
	m.StatePath = os.Getenv("CORP_STATE_FILE")
	lat, err := time.ParseDuration(getenv("CORP_LATENCY", "350ms"))
	if err != nil {
		return fmt.Errorf("CORP_LATENCY: %w", err)
	}
	m.Latency = lat
	if err := m.Load(); err != nil {
		return err
	}
	addr := getenv("HTTP_ADDR", ":8090")
	srv := &http.Server{Addr: addr, Handler: m.Handler(token), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() {
		slog.Info("corp-mcp listening", "addr", addr, "tools", len(m.Server.Tools()))
		errc <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
