// Package app holds the checkout context's use cases and their ports.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/checkout/domain"
	"github.com/renanporto/payment-engine/internal/kernel"
	"github.com/renanporto/payment-engine/internal/psp"
)

// ---- Input ports.

// Scope identifies a workspace on behalf of its account (tenant isolation).
type Scope struct{ AccountID, WorkspaceID uuid.UUID }

type Checkouts interface {
	Create(ctx context.Context, s Scope, in CreateInput) (domain.Checkout, error)
	Get(ctx context.Context, s Scope, id uuid.UUID) (domain.Checkout, error)
	List(ctx context.Context, s Scope, p kernel.Page) ([]domain.Checkout, error)
	Disable(ctx context.Context, s Scope, id uuid.UUID) (domain.Checkout, error)
	Reactivate(ctx context.Context, s Scope, id uuid.UUID) (domain.Checkout, error)
	Cancel(ctx context.Context, s Scope, id uuid.UUID) (domain.Checkout, error)
}

// Reserver is offered to other contexts (billing): it decides whether one more
// payment fits a checkout. Call it inside the caller's transaction.
type Reserver interface {
	Reserve(ctx context.Context, workspaceID, id uuid.UUID, method kernel.PaymentMethod) error
}

type CreateInput struct {
	Name              string
	Items             []domain.Item
	PaymentMethods    []kernel.PaymentMethod
	MaxUses           *int32
	ExpiresAt         *time.Time
	ExternalReference *string
}

// ---- Output ports.

// Workspace is checkout's read model of a workspace.
type Workspace struct {
	ID               uuid.UUID
	Active           bool
	ConnectedAccount string
}

type WorkspaceReader interface {
	Workspace(ctx context.Context, accountID, id uuid.UUID, provider string) (Workspace, error)
}

type Repository interface {
	Create(ctx context.Context, c domain.Checkout) (domain.Checkout, error)
	Get(ctx context.Context, workspaceID, id uuid.UUID) (domain.Checkout, error)
	GetForUpdate(ctx context.Context, workspaceID, id uuid.UUID) (domain.Checkout, error)
	List(ctx context.Context, workspaceID uuid.UUID, p kernel.Page) ([]domain.Checkout, error)
	Save(ctx context.Context, c *domain.Checkout) error
}

// Usage counts payments made through a checkout.
type Usage interface {
	CountUses(ctx context.Context, checkoutID uuid.UUID) (int64, error)
}

// PaymentLinks remembers which PSP link backs each checkout.
type PaymentLinks interface {
	Link(ctx context.Context, checkoutID uuid.UUID, provider, externalID string) error
}

type Gateway interface {
	Name() string
	CreateCheckout(ctx context.Context, idempotencyKey, connectedAccount string, c psp.Checkout) (externalID, url string, err error)
}
