// Command apikey issues an API key for a source (consumer):
//
//	go run ./cmd/apikey <source>
//
// It prints the key once; only its hash is stored. Revoke with
// DELETE FROM api_keys WHERE source = '<source>'.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/renanporto/payment-engine/internal/db"
	"github.com/renanporto/payment-engine/internal/httpx"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] == "" {
		fmt.Fprintln(os.Stderr, "usage: apikey <source>")
		os.Exit(2)
	}
	ctx := context.Background()
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
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	key := "pe_" + hex.EncodeToString(b)
	if err := db.New(pool).APIKeyCreate(ctx, db.APIKeyCreateParams{KeyHash: httpx.KeyHash(key), Source: os.Args[1]}); err != nil {
		slog.Error("create api key", "err", err)
		os.Exit(1)
	}
	fmt.Println(key)
}
