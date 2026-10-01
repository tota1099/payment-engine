package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/renanporto/payment-engine/db/migrations"
)

func main() {
	ctx := context.Background()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://payment:payment@localhost:5432/payment_engine"
	}
	pool, err := pgxpool.New(ctx, url)
	if err == nil {
		err = migrations.Up(ctx, pool)
	}
	if err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}
	slog.Info("migrated")
}
