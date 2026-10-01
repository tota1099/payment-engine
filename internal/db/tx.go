package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/renanporto/payment-engine/internal/kernel"
)

type txKey struct{}

// DB hands out Queries bound to the transaction in ctx, if any.
type DB struct{ Pool *pgxpool.Pool }

// Q returns queries on the ctx transaction, or on the pool outside one.
func (d DB) Q(ctx context.Context) *Queries {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return New(tx)
	}
	return New(d.Pool)
}

// InTx implements kernel.Tx.
func (d DB) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	return pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}

// TxFrom returns the transaction carried by ctx, if any.
func TxFrom(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}

// NotFound turns pgx.ErrNoRows into a domain not-found error.
func NotFound(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return kernel.NotFound(what)
	}
	return err
}
