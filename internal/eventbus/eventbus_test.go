package eventbus

import (
	"context"
	"errors"
	"testing"

	"github.com/renanporto/payment-engine/internal/db"
	"github.com/renanporto/payment-engine/internal/testdb"
)

type happened struct{ N int }

func (happened) EventName() string { return "x_happened" }

func TestPublishCommitsOnlyWithTheBusinessWrite(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	client, err := NewClient(pool, false)
	if err != nil {
		t.Fatal(err)
	}
	d, bus := db.DB{Pool: pool}, Publisher{Client: client}

	if err := bus.Publish(ctx, happened{0}); !errors.Is(err, ErrNoTransaction) {
		t.Fatalf("publish outside a transaction: want ErrNoTransaction, got %v", err)
	}
	boom := errors.New("business write failed")
	err = d.InTx(ctx, func(ctx context.Context) error {
		if err := bus.Publish(ctx, happened{1}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if err := d.InTx(ctx, func(ctx context.Context) error { return bus.Publish(ctx, happened{2}) }); err != nil {
		t.Fatal(err)
	}

	var ns []int
	rows, _ := pool.Query(ctx, "SELECT (args->'payload'->>'N')::int FROM river_job ORDER BY id")
	for rows.Next() {
		var n int
		_ = rows.Scan(&n)
		ns = append(ns, n)
	}
	if len(ns) != 1 || ns[0] != 2 {
		t.Fatalf("jobs = %v; want only the committed event", ns)
	}
}
