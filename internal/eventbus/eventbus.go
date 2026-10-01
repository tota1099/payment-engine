// Package eventbus delivers domain events through River, a Postgres job queue.
// Publish inserts a job in the caller's transaction (a transactional outbox):
// the event exists if and only if the business write committed.
package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/renanporto/payment-engine/internal/db"
	"github.com/renanporto/payment-engine/internal/kernel"
)

// NotifyArgs is the job that tells external consumers about a domain event.
type NotifyArgs struct {
	Event         string          `json:"event"`
	Payload       json.RawMessage `json:"payload"`
	CorrelationID string          `json:"correlation_id,omitempty"`
}

func (NotifyArgs) Kind() string { return "notify_client" }

var ErrNoTransaction = errors.New("eventbus: publish must run inside a transaction")

// Publisher implements the EventPublisher port of every context.
type Publisher struct{ Client *river.Client[pgx.Tx] }

func (p Publisher) Publish(ctx context.Context, events ...kernel.Event) error {
	tx, ok := db.TxFrom(ctx)
	if !ok {
		return ErrNoTransaction
	}
	for _, ev := range events {
		payload, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		args := NotifyArgs{Event: ev.EventName(), Payload: payload, CorrelationID: kernel.CorrelationID(ctx)}
		if _, err := p.Client.InsertTx(ctx, tx, args, nil); err != nil {
			return err
		}
	}
	return nil
}

// NotifyWorker delivers NotifyArgs. River retries with backoff on error and
// discards the job after MaxAttempts.
// ponytail: logs only; deliver signed client webhooks in F2.
type NotifyWorker struct {
	river.WorkerDefaults[NotifyArgs]
}

func (NotifyWorker) Work(ctx context.Context, job *river.Job[NotifyArgs]) error {
	slog.InfoContext(ctx, "client notified", "event", job.Args.Event, "payload", string(job.Args.Payload),
		"correlation_id", job.Args.CorrelationID, "attempt", job.Attempt)
	return nil
}

// Workers registers every job this engine processes.
func Workers() *river.Workers {
	w := river.NewWorkers()
	river.AddWorker(w, &NotifyWorker{})
	return w
}

// NewClient builds a River client. work=false gives an insert-only client
// (the API); work=true also processes jobs (the worker).
func NewClient(pool *pgxpool.Pool, work bool) (*river.Client[pgx.Tx], error) {
	cfg := &river.Config{Logger: slog.Default()}
	if work {
		cfg.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 10}}
		cfg.Workers = Workers()
	}
	return river.NewClient(riverpgxv5.New(pool), cfg)
}
