package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/billing/domain"
	"github.com/renanporto/payment-engine/internal/kernel"
)

type QueryInteractor struct {
	Workspaces WorkspaceReader
	Checkouts  CheckoutReader
	Bills      Bills
	Ledger     Ledger
	Provider   string // which PSP's connected account to read
}

var _ Queries = (*QueryInteractor)(nil)

func (uc *QueryInteractor) Transactions(ctx context.Context, s Scope, billID uuid.UUID) ([]domain.Transaction, error) {
	if _, err := uc.Workspaces.Workspace(ctx, s.AccountID, s.WorkspaceID, uc.Provider); err != nil {
		return nil, err
	}
	b, err := uc.Bills.Get(ctx, s.WorkspaceID, billID)
	if err != nil {
		return nil, err
	}
	return uc.Ledger.List(ctx, b.ID)
}

func (uc *QueryInteractor) ListByCheckout(ctx context.Context, s Scope, checkoutID uuid.UUID, p kernel.Page) ([]domain.Bill, error) {
	if _, err := uc.Workspaces.Workspace(ctx, s.AccountID, s.WorkspaceID, uc.Provider); err != nil {
		return nil, err
	}
	ok, err := uc.Checkouts.CheckoutExists(ctx, s.WorkspaceID, checkoutID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, kernel.NotFound("checkout")
	}
	return uc.Bills.ListByCheckout(ctx, s.WorkspaceID, checkoutID, p)
}
