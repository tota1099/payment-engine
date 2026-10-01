// Command worker processes River jobs: today, client notifications of domain events.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/renanporto/payment-engine/internal/eventbus"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://payment:payment@localhost:5432/payment_engine"
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		slog.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	client, err := eventbus.NewClient(pool, true)
	if err == nil {
		err = client.Start(ctx)
	}
	if err != nil {
		slog.Error("river start", "err", err)
		os.Exit(1)
	}
	slog.Info("worker started")
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Stop(shutdown); err != nil {
		slog.Error("river stop", "err", err)
	}
}
