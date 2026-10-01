// Package rest is the checkout context's HTTP driver.
package rest

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/checkout/app"
	"github.com/renanporto/payment-engine/internal/checkout/domain"
	"github.com/renanporto/payment-engine/internal/httpx"
	"github.com/renanporto/payment-engine/internal/kernel"
)

type Handler struct{ Checkouts app.Checkouts }

func (h *Handler) Routes(mux *http.ServeMux, account httpx.Guard) {
	mux.HandleFunc("POST /api/v1/workspaces/{id}/checkouts", account(h.create))
	mux.HandleFunc("GET /api/v1/workspaces/{id}/checkouts", account(h.list))
	mux.HandleFunc("GET /api/v1/workspaces/{id}/checkouts/{cid}", account(h.serve(h.Checkouts.Get)))
	mux.HandleFunc("POST /api/v1/workspaces/{id}/checkouts/{cid}/disable", account(h.serve(h.Checkouts.Disable)))
	mux.HandleFunc("POST /api/v1/workspaces/{id}/checkouts/{cid}/reactivate", account(h.serve(h.Checkouts.Reactivate)))
	mux.HandleFunc("POST /api/v1/workspaces/{id}/checkouts/{cid}/cancel", account(h.serve(h.Checkouts.Cancel)))
}

type itemJSON struct {
	Name     string `json:"name"`
	Amount   int64  `json:"amount"`
	Quantity int    `json:"quantity"`
}

type response struct {
	ID                uuid.UUID              `json:"id"`
	WorkspaceID       uuid.UUID              `json:"workspace_id"`
	Name              string                 `json:"name"`
	Items             []itemJSON             `json:"items"`
	PaymentMethods    []kernel.PaymentMethod `json:"payment_methods"`
	MaxUses           *int32                 `json:"max_uses"`
	ExpiresAt         *time.Time             `json:"expires_at"`
	ExternalReference *string                `json:"external_reference"`
	URL               string                 `json:"url"`
	Status            domain.Status          `json:"status"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

func present(c domain.Checkout) response {
	items := make([]itemJSON, len(c.Items))
	for i, it := range c.Items {
		items[i] = itemJSON(it)
	}
	return response{
		ID: c.ID, WorkspaceID: c.WorkspaceID, Name: c.Name, Items: items, PaymentMethods: c.PaymentMethods,
		MaxUses: c.MaxUses, ExpiresAt: c.ExpiresAt, ExternalReference: c.ExternalReference, URL: c.URL,
		Status: c.Status, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func scope(r *http.Request) (app.Scope, error) {
	wsID, err := httpx.PathUUID(r, "id")
	return app.Scope{AccountID: httpx.AccountID(r.Context()), WorkspaceID: wsID}, err
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	s, err := scope(r)
	if err != nil {
		return err
	}
	var in struct {
		Name              string                 `json:"name"`
		Items             []itemJSON             `json:"items"`
		PaymentMethods    []kernel.PaymentMethod `json:"payment_methods"`
		MaxUses           *int32                 `json:"max_uses"`
		ExpiresAt         *time.Time             `json:"expires_at"`
		ExternalReference *string                `json:"external_reference"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	items := make([]domain.Item, len(in.Items))
	for i, it := range in.Items {
		items[i] = domain.Item(it)
	}
	c, err := h.Checkouts.Create(r.Context(), s, app.CreateInput{
		Name: in.Name, Items: items, PaymentMethods: in.PaymentMethods,
		MaxUses: in.MaxUses, ExpiresAt: in.ExpiresAt, ExternalReference: in.ExternalReference,
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, present(c))
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	s, err := scope(r)
	if err != nil {
		return err
	}
	return httpx.List(w, r, func(p kernel.Page) ([]domain.Checkout, error) {
		return h.Checkouts.List(r.Context(), s, p)
	}, func(c domain.Checkout) uuid.UUID { return c.ID }, present)
}

// serve handles routes on one checkout: {id} workspace, {cid} checkout.
func (h *Handler) serve(op func(context.Context, app.Scope, uuid.UUID) (domain.Checkout, error)) httpx.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		s, err := scope(r)
		if err != nil {
			return err
		}
		id, err := httpx.PathUUID(r, "cid")
		if err != nil {
			return err
		}
		c, err := op(r.Context(), s, id)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, present(c))
		return nil
	}
}
