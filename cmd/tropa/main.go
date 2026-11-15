// Command tropa runs the AI-buddy API and serves the web UI.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"tropa/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "tropa:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := app.LoadConfig()
	if err != nil {
		return err
	}
	a, err := app.New(ctx, cfg, nil, app.NewLogger(cfg.LogLevel))
	if err != nil {
		return err
	}
	defer a.Close()
	return a.Run(ctx)
}
