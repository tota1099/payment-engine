package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/billing/domain"
	"github.com/renanporto/payment-engine/internal/kernel"
	"github.com/renanporto/payment-engine/internal/psp"
)

var byChargeStatus = map[string]domain.Event{
	psp.ChargeAuthorized: domain.Authorize,
	psp.ChargeCaptured:   domain.Capture,
	psp.ChargeDeclined:   domain.Fail,
}

var byPSPEvent = map[string]domain.Event{
	psp.EventBillAuthorized:  domain.Authorize,
	psp.EventBillCaptured:    domain.Capture,
	psp.EventBillFailed:      domain.Fail,
	psp.EventBillVoided:      domain.Void,
	psp.EventBillChargedBack: domain.ChargeBack,
}

type PaymentInteractor struct {
	Workspaces WorkspaceReader
	Checkouts  CheckoutReserver
	Bills      Bills
	Ledger     Ledger
	Charges    Charges
	PSP        Acquirer
	Events     EventPublisher
	Tx         kernel.Tx
}

var _ Payments = (*PaymentInteractor)(nil)

// OneShot re-attempts the PSP call on retry only if it never got through.
func (uc *PaymentInteractor) OneShot(ctx context.Context, in OneShotInput) (OneShotOutput, error) {
	req, err := domain.NewOneShot(domain.OneShotParams{
		WorkspaceID: in.WorkspaceID, CheckoutID: in.CheckoutID, Amount: in.Amount, PaymentMethod: in.PaymentMethod,
		Installments: in.Installments, TokenID: in.TokenID, IdempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		return OneShotOutput{}, err
	}
	ws, err := uc.Workspaces.Workspace(ctx, in.AccountID, in.WorkspaceID, uc.PSP.Name())
	if err != nil {
		return OneShotOutput{}, err
	}
	if !ws.Active {
		return OneShotOutput{}, kernel.Invalid("workspace is not active")
	}
	out, err := uc.create(ctx, req)
	if err != nil {
		return out, err
	}
	charged, err := uc.Charges.Charged(ctx, out.Bill.ID, uc.PSP.Name())
	if err != nil || charged {
		return out, err
	}

	b := out.Bill
	res, err := uc.PSP.Charge(ctx, psp.ChargeRequest{
		IdempotencyKey: b.ID.String(), ConnectedAccount: ws.ConnectedAccount, Amount: b.Amount, Currency: b.Currency,
		PaymentMethod: string(b.PaymentMethod), Installments: b.Installments, TokenID: in.TokenID,
	})
	if err != nil {
		slog.ErrorContext(ctx, "psp charge", "bill_id", b.ID, "err", err)
		return out, kernel.Unavailable("payment provider unavailable; retry with the same Idempotency-Key")
	}
	err = uc.Tx(ctx, func(ctx context.Context) error {
		if err := uc.Charges.Link(ctx, b.ID, uc.PSP.Name(), res.ExternalID); err != nil {
			return err
		}
		out.Bill, err = uc.record(ctx, b.ID, byChargeStatus[res.Status], "charge", res.Code, res.CreatedAt)
		// A webhook may have moved the bill first; the PSP's later word wins.
		if errors.Is(err, kernel.ErrInvalidTransition) {
			out.Bill, err = uc.Bills.GetForUpdate(ctx, b.ID)
		}
		return err
	})
	return out, err
}

// create inserts the bill, or returns the one already under its idempotency key.
func (uc *PaymentInteractor) create(ctx context.Context, req domain.Bill) (out OneShotOutput, err error) {
	if out.Bill, err = uc.replay(ctx, req); !errors.Is(err, kernel.ErrNotFound) {
		return out, err
	}
	err = uc.Tx(ctx, func(ctx context.Context) error {
		if req.CheckoutID != nil {
			if err := uc.Checkouts.Reserve(ctx, req.WorkspaceID, *req.CheckoutID, req.PaymentMethod); err != nil {
				return err
			}
		}
		out.Bill, out.Created, err = uc.Bills.Insert(ctx, req)
		return err
	})
	if err == nil && !out.Created { // lost a race on the same key
		out.Bill, err = uc.replay(ctx, req)
	}
	return out, err
}

func (uc *PaymentInteractor) replay(ctx context.Context, req domain.Bill) (domain.Bill, error) {
	b, err := uc.Bills.ByKey(ctx, req.WorkspaceID, req.IdempotencyKey)
	if err == nil && !b.SameRequest(req) {
		return b, kernel.Invalid("Idempotency-Key reused with a different payload")
	}
	return b, err
}

func (uc *PaymentInteractor) ApplyEvent(ctx context.Context, billID uuid.UUID, ev psp.Event) error {
	event, ok := byPSPEvent[ev.Type]
	if !ok {
		return kernel.Invalid("unsupported bill event %s", ev.Type)
	}
	code := ev.Code
	if code == "" {
		code = ev.Type
	}
	return uc.Tx(ctx, func(ctx context.Context) error {
		_, err := uc.record(ctx, billID, event, ev.ID, code, ev.OccurredAt)
		return err
	})
}

// record locks the bill, applies event (none: async charge still pending),
// appends the PSP fact to the ledger and publishes what changed. Runs inside a Tx.
func (uc *PaymentInteractor) record(ctx context.Context, billID uuid.UUID, event domain.Event, key, code string, at time.Time) (domain.Bill, error) {
	b, err := uc.Bills.GetForUpdate(ctx, billID)
	if err != nil {
		return b, err
	}
	if event != "" {
		if err := b.Apply(event, at); err != nil {
			return b, err
		}
	}
	if err := uc.Ledger.Append(ctx, domain.Transaction{
		BillID: b.ID, Amount: b.Amount, Status: b.Status, Code: code, Provider: uc.PSP.Name(),
		IdempotencyKey: key, ProviderCreatedAt: at,
	}); err != nil {
		return b, err
	}
	events := b.Pull()
	if len(events) == 0 {
		return b, nil
	}
	if err := uc.Bills.Save(ctx, &b); err != nil {
		return b, err
	}
	return b, uc.Events.Publish(ctx, events...)
}
