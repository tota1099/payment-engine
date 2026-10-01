// Package rest is the account context's HTTP driver: it decodes requests,
// calls the input ports and presents the result.
package rest

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/account/app"
	"github.com/renanporto/payment-engine/internal/account/domain"
	"github.com/renanporto/payment-engine/internal/httpx"
	"github.com/renanporto/payment-engine/internal/kernel"
)

type Handler struct {
	Accounts   app.Accounts
	Workspaces app.Workspaces
}

// Routes mounts the context's endpoints. consumer requires a known consumer;
// account also requires it to own X-Account-Id.
func (h *Handler) Routes(mux *http.ServeMux, consumer, account httpx.Guard) {
	mux.HandleFunc("POST /api/v1/accounts", consumer(h.createAccount))
	mux.HandleFunc("GET /api/v1/accounts/{id}", account(h.showAccount))
	mux.HandleFunc("POST /api/v1/accounts/{id}/accredit", account(h.accreditAccount))
	mux.HandleFunc("POST /api/v1/workspaces", account(h.createWorkspace))
	mux.HandleFunc("GET /api/v1/workspaces", account(h.listWorkspaces))
	mux.HandleFunc("GET /api/v1/workspaces/{id}", account(h.showWorkspace))
	mux.HandleFunc("POST /api/v1/workspaces/{id}/accredit", account(h.accreditWorkspace))
}

// Sources backs httpx.AccountGuard.
func (h *Handler) Sources(ctx context.Context, id uuid.UUID) ([]string, error) {
	a, err := h.Accounts.Get(ctx, id)
	return a.Sources, err
}

type accountResponse struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Email          string    `json:"email"`
	DocumentType   string    `json:"document_type"`
	DocumentNumber string    `json:"document_number"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func presentAccount(a domain.Account) accountResponse {
	return accountResponse{
		ID: a.ID, Name: a.Name, Email: a.Email, DocumentType: a.Document.Type, DocumentNumber: a.Document.Number,
		Status: string(a.Status), CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

type workspaceResponse struct {
	ID             uuid.UUID `json:"id"`
	AccountID      uuid.UUID `json:"account_id"`
	Name           string    `json:"name"`
	Email          string    `json:"email"`
	DocumentType   string    `json:"document_type"`
	DocumentNumber string    `json:"document_number"`
	MCC            string    `json:"mcc"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func presentWorkspace(w domain.Workspace) workspaceResponse {
	return workspaceResponse{
		ID: w.ID, AccountID: w.AccountID, Name: w.Name, Email: w.Email, DocumentType: w.Document.Type,
		DocumentNumber: w.Document.Number, MCC: w.MCC, Status: string(w.Status), CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt,
	}
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name           string `json:"name"`
		Email          string `json:"email"`
		DocumentType   string `json:"document_type"`
		DocumentNumber string `json:"document_number"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	a, err := h.Accounts.Create(r.Context(), app.CreateAccountInput{
		Source: httpx.Source(r.Context()), Name: in.Name, Email: in.Email,
		DocumentType: in.DocumentType, DocumentNumber: in.DocumentNumber,
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, presentAccount(a))
	return nil
}

// pathAccount is the {id} account, which must be the authenticated one.
func pathAccount(r *http.Request) (uuid.UUID, error) {
	id, err := httpx.PathUUID(r, "id")
	if err != nil || id != httpx.AccountID(r.Context()) {
		return id, kernel.NotFound("account")
	}
	return id, nil
}

func (h *Handler) showAccount(w http.ResponseWriter, r *http.Request) error {
	return h.serveAccount(w, r, h.Accounts.Get)
}

func (h *Handler) accreditAccount(w http.ResponseWriter, r *http.Request) error {
	return h.serveAccount(w, r, h.Accounts.Accredit)
}

func (h *Handler) serveAccount(w http.ResponseWriter, r *http.Request, op func(context.Context, uuid.UUID) (domain.Account, error)) error {
	id, err := pathAccount(r)
	if err != nil {
		return err
	}
	a, err := op(r.Context(), id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, presentAccount(a))
	return nil
}

func (h *Handler) createWorkspace(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name           string `json:"name"`
		Email          string `json:"email"`
		DocumentType   string `json:"document_type"`
		DocumentNumber string `json:"document_number"`
		MCC            string `json:"mcc"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ws, err := h.Workspaces.Create(r.Context(), app.CreateWorkspaceInput{
		AccountID: httpx.AccountID(r.Context()), Name: in.Name, Email: in.Email,
		DocumentType: in.DocumentType, DocumentNumber: in.DocumentNumber, MCC: in.MCC,
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, presentWorkspace(ws))
	return nil
}

func (h *Handler) listWorkspaces(w http.ResponseWriter, r *http.Request) error {
	return httpx.List(w, r, func(p kernel.Page) ([]domain.Workspace, error) {
		return h.Workspaces.List(r.Context(), httpx.AccountID(r.Context()), p)
	}, func(ws domain.Workspace) uuid.UUID { return ws.ID }, presentWorkspace)
}

func (h *Handler) showWorkspace(w http.ResponseWriter, r *http.Request) error {
	return h.serveWorkspace(w, r, h.Workspaces.Get)
}

func (h *Handler) accreditWorkspace(w http.ResponseWriter, r *http.Request) error {
	return h.serveWorkspace(w, r, h.Workspaces.Accredit)
}

func (h *Handler) serveWorkspace(w http.ResponseWriter, r *http.Request, op func(ctx context.Context, accountID, id uuid.UUID) (domain.Workspace, error)) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	ws, err := op(r.Context(), httpx.AccountID(r.Context()), id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, presentWorkspace(ws))
	return nil
}
