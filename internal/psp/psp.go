// Package psp is the only seam to payment service providers. Contexts speak
// this neutral vocabulary; each adapter (fake, later stripe) maps it to its API.
//
// Shapes follow Stripe: a workspace is a connected account, a checkout is a
// hosted payment link, a bill charge is a PaymentIntent, amounts are int64 cents,
// and every mutating call carries an idempotency key.
package psp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type Provider interface {
	Name() string
	// CreateConnectedAccount registers the workspace as a seller. Activation
	// arrives later as a workspace.activated / workspace.rejected event.
	CreateConnectedAccount(ctx context.Context, idempotencyKey string, w Workspace) (externalID string, err error)
	CreateCheckout(ctx context.Context, idempotencyKey, connectedAccount string, c Checkout) (externalID, url string, err error)
	Charge(ctx context.Context, req ChargeRequest) (ChargeResult, error)
	// ParseWebhook authenticates the request (signature + replay window) and decodes it.
	ParseWebhook(header http.Header, body []byte) (Event, error)
}

type Workspace struct {
	Name, Email, DocumentType, DocumentNumber, MCC string
}

type Checkout struct {
	Name           string
	PaymentMethods []string
}

type ChargeRequest struct {
	IdempotencyKey   string
	ConnectedAccount string
	Amount           int64
	Currency         string
	PaymentMethod    string // credit_card | pix | boleto
	Installments     int32
	TokenID          string // card tokenized client-side; raw PAN never reaches us
}

// Charge outcomes.
const (
	ChargePending    = "pending" // async methods (pix, boleto): wait for webhook
	ChargeAuthorized = "authorized"
	ChargeCaptured   = "captured"
	ChargeDeclined   = "declined"
)

type ChargeResult struct {
	ExternalID string
	Status     string
	Code       string // provider decline/approval code
	CreatedAt  time.Time
}

// Event types the engine understands.
const (
	EventWorkspaceActivated = "workspace.activated"
	EventWorkspaceRejected  = "workspace.rejected"
	EventBillAuthorized     = "bill.authorized"
	EventBillCaptured       = "bill.captured"
	EventBillFailed         = "bill.failed"
	EventBillVoided         = "bill.voided"
	EventBillChargedBack    = "bill.charged_back"
)

type Event struct {
	ID         string          `json:"id"` // provider event id, used for idempotency
	Type       string          `json:"type"`
	ExternalID string          `json:"external_id"` // provider id of the affected object
	Code       string          `json:"code,omitempty"`
	OccurredAt time.Time       `json:"occurred_at"`
	Raw        json.RawMessage `json:"-"`
}

var ErrInvalidWebhook = errors.New("psp: invalid webhook")
