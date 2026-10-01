// Package rest is the billing context's HTTP driver.
package rest

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/billing/app"
	"github.com/renanporto/payment-engine/internal/billing/domain"
	"github.com/renanporto/payment-engine/internal/httpx"
	"github.com/renanporto/payment-engine/internal/kernel"
)

type Handler struct {
	Payments app.Payments
	Queries  app.Queries
}

func (h *Handler) Routes(mux *http.ServeMux, account httpx.Guard) {
	mux.HandleFunc("POST /api/v1/workspaces/{id}/bills", account(h.oneShot))
	mux.HandleFunc("GET /api/v1/workspaces/{id}/bills/{bid}/transactions", account(h.transactions))
	mux.HandleFunc("GET /api/v1/workspaces/{id}/checkouts/{cid}/bills", account(h.listByCheckout))
}

type billResponse struct {
	ID            uuid.UUID            `json:"id"`
	WorkspaceID   uuid.UUID            `json:"workspace_id"`
	CheckoutID    *uuid.UUID           `json:"checkout_id"`
	Amount        int64                `json:"amount"`
	Currency      string               `json:"currency"`
	PaymentMethod kernel.PaymentMethod `json:"payment_method"`
	Installments  int32                `json:"installments"`
	Status        domain.Status        `json:"status"`
	CapturedAt    *time.Time           `json:"captured_at"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

func presentBill(b domain.Bill) billResponse {
	return billResponse{
		ID: b.ID, WorkspaceID: b.WorkspaceID, CheckoutID: b.CheckoutID, Amount: b.Amount, Currency: b.Currency,
		PaymentMethod: b.PaymentMethod, Installments: b.Installments, Status: b.Status, CapturedAt: b.CapturedAt,
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
	}
}

type transactionResponse struct {
	ID                uuid.UUID     `json:"id"`
	Amount            int64         `json:"amount"`
	Status            domain.Status `json:"status"`
	Code              string        `json:"code"`
	Provider          string        `json:"provider"`
	ProviderCreatedAt time.Time     `json:"provider_created_at"`
	CreatedAt         time.Time     `json:"created_at"`
}

func scope(r *http.Request) (app.Scope, error) {
	wsID, err := httpx.PathUUID(r, "id")
	return app.Scope{AccountID: httpx.AccountID(r.Context()), WorkspaceID: wsID}, err
}

func (h *Handler) oneShot(w http.ResponseWriter, r *http.Request) error {
	s, err := scope(r)
	if err != nil {
		return err
	}
	var in struct {
		Amount        int64                `json:"amount"`
		PaymentMethod kernel.PaymentMethod `json:"payment_method"`
		Installments  int32                `json:"installments"`
		TokenID       string               `json:"token_id"`
		CheckoutID    *uuid.UUID           `json:"checkout_id"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	out, err := h.Payments.OneShot(r.Context(), app.OneShotInput{
		Scope: s, IdempotencyKey: r.Header.Get("Idempotency-Key"), Amount: in.Amount, PaymentMethod: in.PaymentMethod,
		Installments: in.Installments, TokenID: in.TokenID, CheckoutID: in.CheckoutID,
	})
	if err != nil {
		return err
	}
	status := http.StatusOK // replay
	if out.Created {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, presentBill(out.Bill))
	return nil
}

func (h *Handler) transactions(w http.ResponseWriter, r *http.Request) error {
	s, err := scope(r)
	if err != nil {
		return err
	}
	billID, err := httpx.PathUUID(r, "bid")
	if err != nil {
		return err
	}
	txs, err := h.Queries.Transactions(r.Context(), s, billID)
	if err != nil {
		return err
	}
	out := make([]transactionResponse, len(txs))
	for i, t := range txs {
		out[i] = transactionResponse{
			ID: t.ID, Amount: t.Amount, Status: t.Status, Code: t.Code, Provider: t.Provider,
			ProviderCreatedAt: t.ProviderCreatedAt, CreatedAt: t.CreatedAt,
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}

func (h *Handler) listByCheckout(w http.ResponseWriter, r *http.Request) error {
	s, err := scope(r)
	if err != nil {
		return err
	}
	checkoutID, err := httpx.PathUUID(r, "cid")
	if err != nil {
		return err
	}
	return httpx.List(w, r, func(p kernel.Page) ([]domain.Bill, error) {
		return h.Queries.ListByCheckout(r.Context(), s, checkoutID, p)
	}, func(b domain.Bill) uuid.UUID { return b.ID }, presentBill)
}
