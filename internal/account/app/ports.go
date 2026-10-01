// Package app holds the account context's use cases (interactors) and the
// ports they expose (input) and depend on (output).
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/account/domain"
	"github.com/renanporto/payment-engine/internal/kernel"
	"github.com/renanporto/payment-engine/internal/psp"
)

// ---- Input ports: what drivers (REST, webhooks) may ask of this context.

type Accounts interface {
	Create(ctx context.Context, in CreateAccountInput) (domain.Account, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Account, error)
	Accredit(ctx context.Context, id uuid.UUID) (domain.Account, error)
}

type Workspaces interface {
	Create(ctx context.Context, in CreateWorkspaceInput) (domain.Workspace, error)
	Get(ctx context.Context, accountID, id uuid.UUID) (domain.Workspace, error)
	List(ctx context.Context, accountID uuid.UUID, p kernel.Page) ([]domain.Workspace, error)
	Accredit(ctx context.Context, accountID, id uuid.UUID) (domain.Workspace, error)
	// ApplyOnboardingEvent consumes the PSP's verdict on a workspace.
	ApplyOnboardingEvent(ctx context.Context, workspaceID uuid.UUID, ev psp.Event) error
}

type CreateAccountInput struct {
	Source                                    string
	Name, Email, DocumentType, DocumentNumber string
}

type CreateWorkspaceInput struct {
	AccountID                                      uuid.UUID
	Name, Email, DocumentType, DocumentNumber, MCC string
}

// ---- Output ports: what this context needs from the outside, one role each.

type AccountRepository interface {
	Create(ctx context.Context, a domain.Account) (domain.Account, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Account, error)
	GetForUpdate(ctx context.Context, id uuid.UUID) (domain.Account, error)
	Save(ctx context.Context, a *domain.Account) error
}

type WorkspaceRepository interface {
	Create(ctx context.Context, w domain.Workspace) (domain.Workspace, error)
	Get(ctx context.Context, accountID, id uuid.UUID) (domain.Workspace, error)
	GetForUpdate(ctx context.Context, id uuid.UUID) (domain.Workspace, error)
	List(ctx context.Context, accountID uuid.UUID, p kernel.Page) ([]domain.Workspace, error)
	Save(ctx context.Context, w *domain.Workspace) error
}

// ConnectedAccounts remembers which PSP account backs each workspace.
type ConnectedAccounts interface {
	Link(ctx context.Context, workspaceID uuid.UUID, provider, externalID string) error
}

type Onboarding interface {
	Name() string
	CreateConnectedAccount(ctx context.Context, idempotencyKey string, w psp.Workspace) (string, error)
}

type EventPublisher interface {
	Publish(ctx context.Context, events ...kernel.Event) error
}
