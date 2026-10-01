package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/account/domain"
	"github.com/renanporto/payment-engine/internal/kernel"
	"github.com/renanporto/payment-engine/internal/psp"
)

type WorkspaceInteractor struct {
	Accounts AccountRepository
	Repo     WorkspaceRepository
	Links    ConnectedAccounts
	PSP      Onboarding
	Events   EventPublisher
	Tx       kernel.Tx
}

var _ Workspaces = (*WorkspaceInteractor)(nil)

func (uc *WorkspaceInteractor) Create(ctx context.Context, in CreateWorkspaceInput) (domain.Workspace, error) {
	doc, err := domain.NewDocument(in.DocumentType, in.DocumentNumber)
	if err != nil {
		return domain.Workspace{}, err
	}
	w, err := domain.NewWorkspace(in.AccountID, in.Name, in.Email, doc, in.MCC)
	if err != nil {
		return w, err
	}
	return uc.Repo.Create(ctx, w)
}

func (uc *WorkspaceInteractor) Get(ctx context.Context, accountID, id uuid.UUID) (domain.Workspace, error) {
	return uc.Repo.Get(ctx, accountID, id)
}

func (uc *WorkspaceInteractor) List(ctx context.Context, accountID uuid.UUID, p kernel.Page) ([]domain.Workspace, error) {
	return uc.Repo.List(ctx, accountID, p)
}

// Accredit registers the workspace at the PSP. Activation arrives by webhook.
func (uc *WorkspaceInteractor) Accredit(ctx context.Context, accountID, id uuid.UUID) (domain.Workspace, error) {
	acc, err := uc.Accounts.Get(ctx, accountID)
	if err != nil {
		return domain.Workspace{}, err
	}
	if !acc.Active() {
		return domain.Workspace{}, kernel.Invalid("account is not active")
	}
	w, err := uc.Repo.Get(ctx, accountID, id)
	if err != nil {
		return w, err
	}
	probe := w // fail fast before calling the PSP
	if err := probe.Accredit(); err != nil {
		return w, err
	}
	extID, err := uc.PSP.CreateConnectedAccount(ctx, w.ID.String(), psp.Workspace{
		Name: w.Name, Email: w.Email, DocumentType: w.Document.Type, DocumentNumber: w.Document.Number, MCC: w.MCC,
	})
	if err != nil {
		slog.ErrorContext(ctx, "psp create connected account", "workspace_id", w.ID, "err", err)
		return w, kernel.Unavailable("payment provider unavailable")
	}
	err = uc.Tx(ctx, func(ctx context.Context) error {
		if w, err = uc.Repo.GetForUpdate(ctx, id); err != nil {
			return err
		}
		if err := w.Accredit(); err != nil {
			return err
		}
		if err := uc.Repo.Save(ctx, &w); err != nil {
			return err
		}
		return uc.Links.Link(ctx, w.ID, uc.PSP.Name(), extID)
	})
	return w, err
}

func (uc *WorkspaceInteractor) ApplyOnboardingEvent(ctx context.Context, workspaceID uuid.UUID, ev psp.Event) error {
	return uc.Tx(ctx, func(ctx context.Context) error {
		w, err := uc.Repo.GetForUpdate(ctx, workspaceID)
		if err != nil {
			return err
		}
		switch ev.Type {
		case psp.EventWorkspaceActivated:
			err = w.Activate()
		case psp.EventWorkspaceRejected:
			err = w.Reject()
		default:
			err = kernel.Invalid("unsupported workspace event %s", ev.Type)
		}
		if err != nil {
			return err
		}
		if err := uc.Repo.Save(ctx, &w); err != nil {
			return err
		}
		return uc.Events.Publish(ctx, w.Pull()...)
	})
}
