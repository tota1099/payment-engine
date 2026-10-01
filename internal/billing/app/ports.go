// Package app holds the billing context's use cases and their ports.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/billing/domain"
	"github.com/renanporto/payment-engine/internal/kernel"
	"github.com/renanporto/payment-engine/internal/psp"
)

// ---- Input ports.

// Scope identifies a workspace on behalf of its account (tenant isolation).
type Scope struct{ AccountID, WorkspaceID uuid.UUID }

type Payments interface {
	// OneShot charges once per idempotency key; a retry returns the same bill.
	OneShot(ctx context.Context, in OneShotInput) (OneShotOutput, error)
	// ApplyEvent consumes a PSP fact about a bill.
	ApplyEvent(ctx context.Context, billID uuid.UUID, ev psp.Event) error
}

type Queries interface {
	Transactions(ctx context.Context, s Scope, billID uuid.UUID) ([]domain.Transaction, error)
	ListByCheckout(ctx context.Context, s Scope, checkoutID uuid.UUID, p kernel.Page) ([]domain.Bill, error)
}

type OneShotInput struct {
	Scope
	IdempotencyKey string
	Amount         int64
	PaymentMethod  kernel.PaymentMethod
	Installments   int32
	TokenID        string
	CheckoutID     *uuid.UUID
}

type OneShotOutput struct {
	Bill    domain.Bill
	Created bool // false: replay of an earlier request with the same key
}

// ---- Output ports.

// Workspace is billing's read model of a workspace.
type Workspace struct {
	ID               uuid.UUID
	Active           bool
	ConnectedAccount string
}

type WorkspaceReader interface {
	Workspace(ctx context.Context, accountID, id uuid.UUID, provider string) (Workspace, error)
}

type CheckoutReader interface {
	CheckoutExists(ctx context.Context, workspaceID, id uuid.UUID) (bool, error)
}

// CheckoutReserver is the checkout context's decision on whether a payment fits.
type CheckoutReserver interface {
	Reserve(ctx context.Context, workspaceID, id uuid.UUID, method kernel.PaymentMethod) error
}

type Bills interface {
	// Insert returns created=false when the idempotency key is already taken.
	Insert(ctx context.Context, b domain.Bill) (out domain.Bill, created bool, err error)
	ByKey(ctx context.Context, workspaceID uuid.UUID, key string) (domain.Bill, error)
	Get(ctx context.Context, workspaceID, id uuid.UUID) (domain.Bill, error)
	GetForUpdate(ctx context.Context, id uuid.UUID) (domain.Bill, error)
	Save(ctx context.Context, b *domain.Bill) error
	ListByCheckout(ctx context.Context, workspaceID, checkoutID uuid.UUID, p kernel.Page) ([]domain.Bill, error)
}

// Ledger is the append-only record of PSP facts per bill.
type Ledger interface {
	// Append is idempotent on (bill, key).
	Append(ctx context.Context, t domain.Transaction) error
	List(ctx context.Context, billID uuid.UUID) ([]domain.Transaction, error)
}

// Charges remembers which PSP charge backs each bill.
type Charges interface {
	Charged(ctx context.Context, billID uuid.UUID, provider string) (bool, error)
	Link(ctx context.Context, billID uuid.UUID, provider, externalID string) error
}

type Acquirer interface {
	Name() string
	Charge(ctx context.Context, req psp.ChargeRequest) (psp.ChargeResult, error)
}

type EventPublisher interface {
	Publish(ctx context.Context, events ...kernel.Event) error
}
