// Package postgres implements the checkout context's output ports on Postgres.
package postgres

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/checkout/app"
	"github.com/renanporto/payment-engine/internal/checkout/domain"
	"github.com/renanporto/payment-engine/internal/db"
	"github.com/renanporto/payment-engine/internal/kernel"
)

var (
	_ app.Repository      = Checkouts{}
	_ app.Usage           = Usage{}
	_ app.WorkspaceReader = Workspaces{}
	_ app.PaymentLinks    = PaymentLinks{}
)

// item is the stored JSON shape of domain.Item.
type item struct {
	Name     string `json:"name"`
	Amount   int64  `json:"amount"`
	Quantity int    `json:"quantity"`
}

func toCheckout(r db.Checkout) (domain.Checkout, error) {
	c := domain.Checkout{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Name: r.Name, MaxUses: r.MaxUses, ExpiresAt: r.ExpiresAt,
		ExternalReference: r.ExternalReference, URL: r.Url, Status: domain.Status(r.Status), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	for _, m := range r.PaymentMethods {
		c.PaymentMethods = append(c.PaymentMethods, kernel.PaymentMethod(m))
	}
	var items []item
	if err := json.Unmarshal(r.Items, &items); err != nil {
		return c, err
	}
	for _, it := range items {
		c.Items = append(c.Items, domain.Item(it))
	}
	return c, nil
}

type Checkouts struct{ DB db.DB }

func (s Checkouts) Create(ctx context.Context, c domain.Checkout) (domain.Checkout, error) {
	items := make([]item, len(c.Items))
	for i, it := range c.Items {
		items[i] = item(it)
	}
	itemsJSON, err := json.Marshal(items)
	if err != nil {
		return c, err
	}
	methods := make([]string, len(c.PaymentMethods))
	for i, m := range c.PaymentMethods {
		methods[i] = string(m)
	}
	r, err := s.DB.Q(ctx).CheckoutCreate(ctx, db.CheckoutCreateParams{
		ID: c.ID, WorkspaceID: c.WorkspaceID, Name: c.Name, Items: itemsJSON, PaymentMethods: methods,
		MaxUses: c.MaxUses, ExpiresAt: c.ExpiresAt, ExternalReference: c.ExternalReference, Url: c.URL,
	})
	if err != nil {
		return c, err
	}
	return toCheckout(r)
}

func (s Checkouts) Get(ctx context.Context, workspaceID, id uuid.UUID) (domain.Checkout, error) {
	r, err := s.DB.Q(ctx).CheckoutGet(ctx, db.CheckoutGetParams{ID: id, WorkspaceID: workspaceID})
	if err != nil {
		return domain.Checkout{}, db.NotFound(err, "checkout")
	}
	return toCheckout(r)
}

func (s Checkouts) GetForUpdate(ctx context.Context, workspaceID, id uuid.UUID) (domain.Checkout, error) {
	r, err := s.DB.Q(ctx).CheckoutGetForUpdate(ctx, db.CheckoutGetForUpdateParams{ID: id, WorkspaceID: workspaceID})
	if err != nil {
		return domain.Checkout{}, db.NotFound(err, "checkout")
	}
	return toCheckout(r)
}

func (s Checkouts) List(ctx context.Context, workspaceID uuid.UUID, p kernel.Page) ([]domain.Checkout, error) {
	rows, err := s.DB.Q(ctx).CheckoutList(ctx, db.CheckoutListParams{WorkspaceID: workspaceID, Limit: p.Limit, StartingAfter: p.StartingAfter})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Checkout, len(rows))
	for i, r := range rows {
		if out[i], err = toCheckout(r); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s Checkouts) Save(ctx context.Context, c *domain.Checkout) (err error) {
	c.UpdatedAt, err = s.DB.Q(ctx).CheckoutUpdateStatus(ctx, db.CheckoutUpdateStatusParams{ID: c.ID, Status: string(c.Status)})
	return err
}

type Usage struct{ DB db.DB }

func (s Usage) CountUses(ctx context.Context, checkoutID uuid.UUID) (int64, error) {
	return s.DB.Q(ctx).CheckoutCountUses(ctx, uuid.NullUUID{UUID: checkoutID, Valid: true})
}

type Workspaces struct{ DB db.DB }

func (s Workspaces) Workspace(ctx context.Context, accountID, id uuid.UUID, provider string) (app.Workspace, error) {
	r, err := s.DB.Q(ctx).CheckoutWorkspace(ctx, db.CheckoutWorkspaceParams{ID: id, AccountID: accountID, Provider: provider})
	return app.Workspace{ID: r.ID, Active: r.Status == "active", ConnectedAccount: r.ConnectedAccount}, db.NotFound(err, "workspace")
}

type PaymentLinks struct{ DB db.DB }

func (s PaymentLinks) Link(ctx context.Context, checkoutID uuid.UUID, provider, externalID string) error {
	return s.DB.Q(ctx).ProviderRefUpsert(ctx, db.ProviderRefUpsertParams{
		EntityType: "checkout", EntityID: checkoutID, Provider: provider, ExternalID: externalID,
	})
}
