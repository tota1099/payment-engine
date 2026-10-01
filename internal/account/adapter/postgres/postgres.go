// Package postgres implements the account context's output ports on Postgres.
package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/account/app"
	"github.com/renanporto/payment-engine/internal/account/domain"
	"github.com/renanporto/payment-engine/internal/db"
	"github.com/renanporto/payment-engine/internal/kernel"
)

var (
	_ app.AccountRepository   = Accounts{}
	_ app.WorkspaceRepository = Workspaces{}
	_ app.ConnectedAccounts   = ConnectedAccounts{}
)

type Accounts struct{ DB db.DB }

func toAccount(r db.Account) domain.Account {
	return domain.Account{
		ID: r.ID, Name: r.Name, Email: r.Email, Document: domain.Document{Type: r.DocumentType, Number: r.DocumentNumber},
		Sources: r.Sources, Status: domain.AccountStatus(r.Status), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func (s Accounts) Create(ctx context.Context, a domain.Account) (domain.Account, error) {
	r, err := s.DB.Q(ctx).AccountCreate(ctx, db.AccountCreateParams{
		Name: a.Name, Email: a.Email, DocumentType: a.Document.Type, DocumentNumber: a.Document.Number, Sources: a.Sources,
	})
	return toAccount(r), err
}

func (s Accounts) Get(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	r, err := s.DB.Q(ctx).AccountGet(ctx, id)
	return toAccount(r), db.NotFound(err, "account")
}

func (s Accounts) GetForUpdate(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	r, err := s.DB.Q(ctx).AccountGetForUpdate(ctx, id)
	return toAccount(r), db.NotFound(err, "account")
}

func (s Accounts) Save(ctx context.Context, a *domain.Account) (err error) {
	a.UpdatedAt, err = s.DB.Q(ctx).AccountUpdateStatus(ctx, db.AccountUpdateStatusParams{ID: a.ID, Status: string(a.Status)})
	return err
}

type Workspaces struct{ DB db.DB }

func toWorkspace(r db.Workspace) domain.Workspace {
	return domain.Workspace{
		ID: r.ID, AccountID: r.AccountID, Name: r.Name, Email: r.Email,
		Document: domain.Document{Type: r.DocumentType, Number: r.DocumentNumber}, MCC: r.Mcc,
		Status: domain.WorkspaceStatus(r.Status), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func (s Workspaces) Create(ctx context.Context, w domain.Workspace) (domain.Workspace, error) {
	r, err := s.DB.Q(ctx).AccountWorkspaceCreate(ctx, db.AccountWorkspaceCreateParams{
		AccountID: w.AccountID, Name: w.Name, Email: w.Email,
		DocumentType: w.Document.Type, DocumentNumber: w.Document.Number, Mcc: w.MCC,
	})
	return toWorkspace(r), err
}

func (s Workspaces) Get(ctx context.Context, accountID, id uuid.UUID) (domain.Workspace, error) {
	r, err := s.DB.Q(ctx).AccountWorkspaceGet(ctx, db.AccountWorkspaceGetParams{ID: id, AccountID: accountID})
	return toWorkspace(r), db.NotFound(err, "workspace")
}

func (s Workspaces) GetForUpdate(ctx context.Context, id uuid.UUID) (domain.Workspace, error) {
	r, err := s.DB.Q(ctx).AccountWorkspaceGetForUpdate(ctx, id)
	return toWorkspace(r), db.NotFound(err, "workspace")
}

func (s Workspaces) List(ctx context.Context, accountID uuid.UUID, p kernel.Page) ([]domain.Workspace, error) {
	rows, err := s.DB.Q(ctx).AccountWorkspaceList(ctx, db.AccountWorkspaceListParams{
		AccountID: accountID, Limit: p.Limit, StartingAfter: p.StartingAfter,
	})
	out := make([]domain.Workspace, len(rows))
	for i, r := range rows {
		out[i] = toWorkspace(r)
	}
	return out, err
}

func (s Workspaces) Save(ctx context.Context, w *domain.Workspace) (err error) {
	w.UpdatedAt, err = s.DB.Q(ctx).AccountWorkspaceUpdateStatus(ctx, db.AccountWorkspaceUpdateStatusParams{ID: w.ID, Status: string(w.Status)})
	return err
}

type ConnectedAccounts struct{ DB db.DB }

func (s ConnectedAccounts) Link(ctx context.Context, workspaceID uuid.UUID, provider, externalID string) error {
	return s.DB.Q(ctx).ProviderRefUpsert(ctx, db.ProviderRefUpsertParams{
		EntityType: "workspace", EntityID: workspaceID, Provider: provider, ExternalID: externalID,
	})
}
