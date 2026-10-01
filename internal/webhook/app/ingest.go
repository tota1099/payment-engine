// Package app holds the webhook context's use case: ingest an authenticated
// PSP event exactly once and hand it to the context that owns the entity.
package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/kernel"
	"github.com/renanporto/payment-engine/internal/psp"
)

// ---- Input port.

type Ingester interface {
	Ingest(ctx context.Context, provider string, ev psp.Event) (Result, error)
}

type Result struct {
	Duplicate bool   // already ingested; nothing done
	Ignored   string // business rejection (e.g. capture after void), acked so the PSP stops retrying
}

// ---- Output ports.

// Inbox stores received events, deduplicated by (provider, event id).
type Inbox interface {
	// Record returns isNew=false for an event seen before.
	Record(ctx context.Context, provider string, ev psp.Event) (id int64, isNew bool, err error)
	MarkProcessed(ctx context.Context, id int64) error
}

// Refs resolves a provider's object id to the entity it backs.
type Refs interface {
	Resolve(ctx context.Context, provider, externalID string) (entityType string, entityID uuid.UUID, err error)
}

// Apply is another context's input port for PSP events about its entities.
type Apply func(ctx context.Context, entityID uuid.UUID, ev psp.Event) error

type IngestInteractor struct {
	Inbox Inbox
	Refs  Refs
	// Routes maps an entity type ("workspace", "bill") to its owner's Apply.
	Routes map[string]Apply
	Tx     kernel.Tx
}

var _ Ingester = (*IngestInteractor)(nil)

// Ingest dedupes, resolves and applies the event in one transaction. An
// unknown object fails with ErrNotFound and rolls back, so the PSP redelivers
// once our own write lands.
func (uc *IngestInteractor) Ingest(ctx context.Context, provider string, ev psp.Event) (res Result, err error) {
	err = uc.Tx(ctx, func(ctx context.Context) error {
		id, isNew, err := uc.Inbox.Record(ctx, provider, ev)
		if err != nil || !isNew {
			res.Duplicate = !isNew
			return err
		}
		entityType, entityID, err := uc.Refs.Resolve(ctx, provider, ev.ExternalID)
		if err != nil {
			return err
		}
		apply, ok := uc.Routes[entityType]
		if !ok {
			err = kernel.Invalid("no route for %s events", entityType)
		} else {
			err = apply(ctx, entityID, ev)
		}
		if errors.Is(err, kernel.ErrInvalid) || errors.Is(err, kernel.ErrInvalidTransition) {
			res.Ignored, err = err.Error(), nil
		}
		if err != nil {
			return err
		}
		return uc.Inbox.MarkProcessed(ctx, id)
	})
	return res, err
}
