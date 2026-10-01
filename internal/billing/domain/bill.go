// Package domain holds the billing entities: Bill (one charge attempt) and
// Transaction (each fact the PSP reported about it). No I/O.
package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/kernel"
)

type Status string

const (
	Pending     Status = "pending"
	Authorized  Status = "authorized"
	Captured    Status = "captured" // the paid moment
	Voided      Status = "voided"
	ChargedBack Status = "charged_back"
	Error       Status = "error"
)

// Event is something that happens to a bill.
type Event string

const (
	Authorize  Event = "authorize"
	Capture    Event = "capture"
	Void       Event = "void"
	ChargeBack Event = "charge_back"
	Fail       Event = "fail"
)

var fsm = kernel.FSM[Status]{
	string(Authorize):  {Pending: Authorized},
	string(Capture):    {Pending: Captured, Authorized: Captured},
	string(Void):       {Pending: Voided, Authorized: Voided},
	string(ChargeBack): {Captured: ChargedBack},
	string(Fail):       {Pending: Error, Authorized: Error, Voided: Error, Captured: Error, ChargedBack: Error},
}

type Bill struct {
	kernel.Events
	ID             uuid.UUID
	WorkspaceID    uuid.UUID
	CheckoutID     *uuid.UUID
	Amount         int64 // cents
	Currency       string
	PaymentMethod  kernel.PaymentMethod
	Installments   int32
	Status         Status
	IdempotencyKey string
	CapturedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type OneShotParams struct {
	WorkspaceID    uuid.UUID
	CheckoutID     *uuid.UUID
	Amount         int64
	PaymentMethod  kernel.PaymentMethod
	Installments   int32
	TokenID        string
	IdempotencyKey string
}

// NewOneShot validates a direct charge request.
func NewOneShot(p OneShotParams) (Bill, error) {
	if p.Installments == 0 {
		p.Installments = 1
	}
	b := Bill{
		WorkspaceID: p.WorkspaceID, CheckoutID: p.CheckoutID, Amount: p.Amount, Currency: "BRL",
		PaymentMethod: p.PaymentMethod, Installments: p.Installments, Status: Pending, IdempotencyKey: p.IdempotencyKey,
	}
	switch {
	case p.IdempotencyKey == "":
		return b, kernel.Invalid("Idempotency-Key header is required")
	case p.Amount <= 0:
		return b, kernel.Invalid("amount must be greater than 0")
	case !p.PaymentMethod.Valid():
		return b, kernel.Invalid("unknown payment_method")
	case p.Installments < 1 || p.Installments > 21:
		return b, kernel.Invalid("installments must be between 1 and 21")
	case p.PaymentMethod == kernel.CreditCard && p.TokenID == "":
		return b, kernel.Invalid("token_id is required for credit_card")
	}
	return b, nil
}

// Apply moves the bill and records BillUpdateCompleted. at is the PSP's time;
// it becomes CapturedAt on capture.
func (b *Bill) Apply(ev Event, at time.Time) error {
	to, err := fsm.Next(b.Status, string(ev))
	if err != nil {
		return err
	}
	b.Status = to
	if to == Captured {
		b.CapturedAt = &at
	}
	b.Record(BillUpdateCompleted{BillID: b.ID, Status: to})
	return nil
}

// SameRequest tells whether a retry under the same idempotency key matches this bill.
func (b Bill) SameRequest(other Bill) bool {
	return b.Amount == other.Amount && b.PaymentMethod == other.PaymentMethod
}

// BillUpdateCompleted: a bill changed status.
type BillUpdateCompleted struct {
	BillID uuid.UUID `json:"bill_id"`
	Status Status    `json:"status"`
}

func (BillUpdateCompleted) EventName() string { return "bill_update_completed" }

// Transaction is one fact the PSP reported about a bill. Append-only.
type Transaction struct {
	ID                uuid.UUID
	BillID            uuid.UUID
	Amount            int64
	Status            Status // bill status after this fact
	Code              string
	Provider          string
	IdempotencyKey    string // "charge" or the PSP event id
	ProviderCreatedAt time.Time
	CreatedAt         time.Time
}
