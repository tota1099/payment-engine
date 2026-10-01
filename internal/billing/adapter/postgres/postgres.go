// Package postgres implements the billing context's output ports on Postgres.
package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/renanporto/payment-engine/internal/billing/app"
	"github.com/renanporto/payment-engine/internal/billing/domain"
	"github.com/renanporto/payment-engine/internal/db"
	"github.com/renanporto/payment-engine/internal/kernel"
)

var (
	_ app.Bills           = Bills{}
	_ app.Ledger          = Ledger{}
	_ app.Charges         = Charges{}
	_ app.WorkspaceReader = Workspaces{}
	_ app.CheckoutReader  = Checkouts{}
)

func toBill(r db.Bill) domain.Bill {
	b := domain.Bill{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Amount: r.Amount, Currency: r.Currency,
		PaymentMethod: kernel.PaymentMethod(r.PaymentMethod), Installments: r.Installments, Status: domain.Status(r.Status),
		IdempotencyKey: r.IdempotencyKey, CapturedAt: r.CapturedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.CheckoutID.Valid {
		b.CheckoutID = &r.CheckoutID.UUID
	}
	return b
}

func nullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

type Bills struct{ DB db.DB }

func (s Bills) Insert(ctx context.Context, b domain.Bill) (domain.Bill, bool, error) {
	r, err := s.DB.Q(ctx).BillingInsert(ctx, db.BillingInsertParams{
		WorkspaceID: b.WorkspaceID, CheckoutID: nullUUID(b.CheckoutID), Amount: b.Amount, Currency: b.Currency,
		PaymentMethod: string(b.PaymentMethod), Installments: b.Installments, IdempotencyKey: b.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) { // ON CONFLICT DO NOTHING
		return b, false, nil
	}
	return toBill(r), err == nil, err
}

func (s Bills) ByKey(ctx context.Context, workspaceID uuid.UUID, key string) (domain.Bill, error) {
	r, err := s.DB.Q(ctx).BillingGetByKey(ctx, db.BillingGetByKeyParams{WorkspaceID: workspaceID, IdempotencyKey: key})
	return toBill(r), db.NotFound(err, "bill")
}

func (s Bills) Get(ctx context.Context, workspaceID, id uuid.UUID) (domain.Bill, error) {
	r, err := s.DB.Q(ctx).BillingGet(ctx, db.BillingGetParams{ID: id, WorkspaceID: workspaceID})
	return toBill(r), db.NotFound(err, "bill")
}

func (s Bills) GetForUpdate(ctx context.Context, id uuid.UUID) (domain.Bill, error) {
	r, err := s.DB.Q(ctx).BillingGetForUpdate(ctx, id)
	return toBill(r), db.NotFound(err, "bill")
}

func (s Bills) Save(ctx context.Context, b *domain.Bill) (err error) {
	b.UpdatedAt, err = s.DB.Q(ctx).BillingUpdate(ctx, db.BillingUpdateParams{ID: b.ID, Status: string(b.Status), CapturedAt: b.CapturedAt})
	return err
}

func (s Bills) ListByCheckout(ctx context.Context, workspaceID, checkoutID uuid.UUID, p kernel.Page) ([]domain.Bill, error) {
	rows, err := s.DB.Q(ctx).BillingListByCheckout(ctx, db.BillingListByCheckoutParams{
		WorkspaceID: workspaceID, CheckoutID: uuid.NullUUID{UUID: checkoutID, Valid: true}, Limit: p.Limit, StartingAfter: p.StartingAfter,
	})
	out := make([]domain.Bill, len(rows))
	for i, r := range rows {
		out[i] = toBill(r)
	}
	return out, err
}

type Ledger struct{ DB db.DB }

func (s Ledger) Append(ctx context.Context, t domain.Transaction) error {
	return s.DB.Q(ctx).BillingInsertTransaction(ctx, db.BillingInsertTransactionParams{
		BillID: t.BillID, Amount: t.Amount, Status: string(t.Status), Code: t.Code, Provider: t.Provider,
		IdempotencyKey: t.IdempotencyKey, ProviderCreatedAt: &t.ProviderCreatedAt,
	})
}

func (s Ledger) List(ctx context.Context, billID uuid.UUID) ([]domain.Transaction, error) {
	rows, err := s.DB.Q(ctx).BillingTransactions(ctx, billID)
	out := make([]domain.Transaction, len(rows))
	for i, r := range rows {
		out[i] = domain.Transaction{
			ID: r.ID, BillID: r.BillID, Amount: r.Amount, Status: domain.Status(r.Status), Code: r.Code,
			Provider: r.Provider, IdempotencyKey: r.IdempotencyKey, CreatedAt: r.CreatedAt,
		}
		if r.ProviderCreatedAt != nil {
			out[i].ProviderCreatedAt = *r.ProviderCreatedAt
		}
	}
	return out, err
}

type Charges struct{ DB db.DB }

func (s Charges) Charged(ctx context.Context, billID uuid.UUID, provider string) (bool, error) {
	return s.DB.Q(ctx).ProviderRefExists(ctx, db.ProviderRefExistsParams{EntityType: "bill", EntityID: billID, Provider: provider})
}

func (s Charges) Link(ctx context.Context, billID uuid.UUID, provider, externalID string) error {
	return s.DB.Q(ctx).ProviderRefUpsert(ctx, db.ProviderRefUpsertParams{
		EntityType: "bill", EntityID: billID, Provider: provider, ExternalID: externalID,
	})
}

type Workspaces struct{ DB db.DB }

func (s Workspaces) Workspace(ctx context.Context, accountID, id uuid.UUID, provider string) (app.Workspace, error) {
	r, err := s.DB.Q(ctx).BillingWorkspace(ctx, db.BillingWorkspaceParams{ID: id, AccountID: accountID, Provider: provider})
	return app.Workspace{ID: r.ID, Active: r.Status == "active", ConnectedAccount: r.ConnectedAccount}, db.NotFound(err, "workspace")
}

type Checkouts struct{ DB db.DB }

func (s Checkouts) CheckoutExists(ctx context.Context, workspaceID, id uuid.UUID) (bool, error) {
	return s.DB.Q(ctx).BillingCheckoutExists(ctx, db.BillingCheckoutExistsParams{ID: id, WorkspaceID: workspaceID})
}
