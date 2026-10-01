package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/checkout/domain"
	"github.com/renanporto/payment-engine/internal/kernel"
	"github.com/renanporto/payment-engine/internal/psp"
)

type Interactor struct {
	Workspaces WorkspaceReader
	Repo       Repository
	Usage      Usage
	Links      PaymentLinks
	PSP        Gateway
	Tx         kernel.Tx
	Now        func() time.Time
}

var (
	_ Checkouts = (*Interactor)(nil)
	_ Reserver  = (*Interactor)(nil)
)

func (uc *Interactor) now() time.Time {
	if uc.Now != nil {
		return uc.Now()
	}
	return time.Now()
}

func (uc *Interactor) workspace(ctx context.Context, s Scope) (Workspace, error) {
	return uc.Workspaces.Workspace(ctx, s.AccountID, s.WorkspaceID, uc.PSP.Name())
}

func (uc *Interactor) Create(ctx context.Context, s Scope, in CreateInput) (domain.Checkout, error) {
	ws, err := uc.workspace(ctx, s)
	if err != nil {
		return domain.Checkout{}, err
	}
	if !ws.Active {
		return domain.Checkout{}, kernel.Invalid("workspace is not active")
	}
	c, err := domain.New(domain.NewParams{
		WorkspaceID: ws.ID, Name: in.Name, Items: in.Items, PaymentMethods: in.PaymentMethods,
		MaxUses: in.MaxUses, ExpiresAt: in.ExpiresAt, ExternalReference: in.ExternalReference,
	}, uc.now())
	if err != nil {
		return c, err
	}
	c.ID, _ = uuid.NewV7()
	methods := make([]string, len(c.PaymentMethods))
	for i, m := range c.PaymentMethods {
		methods[i] = string(m)
	}
	extID, url, err := uc.PSP.CreateCheckout(ctx, c.ID.String(), ws.ConnectedAccount, psp.Checkout{Name: c.Name, PaymentMethods: methods})
	if err != nil {
		slog.ErrorContext(ctx, "psp create checkout", "checkout_id", c.ID, "err", err)
		return c, kernel.Unavailable("payment provider unavailable")
	}
	c.URL = url
	err = uc.Tx(ctx, func(ctx context.Context) error {
		if c, err = uc.Repo.Create(ctx, c); err != nil {
			return err
		}
		return uc.Links.Link(ctx, c.ID, uc.PSP.Name(), extID)
	})
	return c, err
}

func (uc *Interactor) Get(ctx context.Context, s Scope, id uuid.UUID) (domain.Checkout, error) {
	if _, err := uc.workspace(ctx, s); err != nil {
		return domain.Checkout{}, err
	}
	return uc.Repo.Get(ctx, s.WorkspaceID, id)
}

func (uc *Interactor) List(ctx context.Context, s Scope, p kernel.Page) ([]domain.Checkout, error) {
	if _, err := uc.workspace(ctx, s); err != nil {
		return nil, err
	}
	return uc.Repo.List(ctx, s.WorkspaceID, p)
}

func (uc *Interactor) Disable(ctx context.Context, s Scope, id uuid.UUID) (domain.Checkout, error) {
	return uc.transition(ctx, s, id, func(c *domain.Checkout) error { return c.Disable(uc.now()) })
}

func (uc *Interactor) Reactivate(ctx context.Context, s Scope, id uuid.UUID) (domain.Checkout, error) {
	return uc.transition(ctx, s, id, func(c *domain.Checkout) error { return c.Reactivate(uc.now()) })
}

func (uc *Interactor) Cancel(ctx context.Context, s Scope, id uuid.UUID) (domain.Checkout, error) {
	return uc.transition(ctx, s, id, (*domain.Checkout).Cancel)
}

func (uc *Interactor) transition(ctx context.Context, s Scope, id uuid.UUID, apply func(*domain.Checkout) error) (c domain.Checkout, err error) {
	if _, err := uc.workspace(ctx, s); err != nil {
		return c, err
	}
	err = uc.Tx(ctx, func(ctx context.Context) error {
		if c, err = uc.Repo.GetForUpdate(ctx, s.WorkspaceID, id); err != nil {
			return err
		}
		if err := apply(&c); err != nil {
			return err
		}
		return uc.Repo.Save(ctx, &c)
	})
	return c, err
}

// Reserve locks the checkout so max_uses holds under concurrent payments.
func (uc *Interactor) Reserve(ctx context.Context, workspaceID, id uuid.UUID, method kernel.PaymentMethod) error {
	return uc.Tx(ctx, func(ctx context.Context) error {
		c, err := uc.Repo.GetForUpdate(ctx, workspaceID, id)
		if err != nil {
			return err
		}
		uses, err := uc.Usage.CountUses(ctx, c.ID)
		if err != nil {
			return err
		}
		return c.Accepts(method, uses, uc.now())
	})
}
