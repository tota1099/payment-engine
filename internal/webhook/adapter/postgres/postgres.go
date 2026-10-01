// Package postgres implements the webhook context's output ports on Postgres.
package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/renanporto/payment-engine/internal/db"
	"github.com/renanporto/payment-engine/internal/psp"
	"github.com/renanporto/payment-engine/internal/webhook/app"
)

var (
	_ app.Inbox = Inbox{}
	_ app.Refs  = Refs{}
)

type Inbox struct{ DB db.DB }

func (s Inbox) Record(ctx context.Context, provider string, ev psp.Event) (int64, bool, error) {
	id, err := s.DB.Q(ctx).WebhookInsert(ctx, db.WebhookInsertParams{
		Provider: provider, IdempotencyKey: ev.ID, EventType: ev.Type, Payload: ev.Raw,
	})
	if errors.Is(err, pgx.ErrNoRows) { // ON CONFLICT DO NOTHING: seen before
		return 0, false, nil
	}
	return id, err == nil, err
}

func (s Inbox) MarkProcessed(ctx context.Context, id int64) error {
	return s.DB.Q(ctx).WebhookMarkProcessed(ctx, id)
}

type Refs struct{ DB db.DB }

func (s Refs) Resolve(ctx context.Context, provider, externalID string) (string, uuid.UUID, error) {
	r, err := s.DB.Q(ctx).ProviderRefFind(ctx, db.ProviderRefFindParams{Provider: provider, ExternalID: externalID})
	return r.EntityType, r.EntityID, db.NotFound(err, "object "+externalID)
}
