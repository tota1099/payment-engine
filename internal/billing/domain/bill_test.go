package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/kernel"
)

func TestBillApply(t *testing.T) {
	cases := []struct {
		from Status
		ev   Event
		to   Status // empty: rejected
	}{
		{Pending, Authorize, Authorized},
		{Pending, Capture, Captured},
		{Authorized, Capture, Captured},
		{Authorized, Void, Voided},
		{Captured, ChargeBack, ChargedBack},
		{ChargedBack, Fail, Error},
		{Captured, Void, ""},
		{Voided, Capture, ""},
		{Error, Authorize, ""},
	}
	for _, c := range cases {
		b := Bill{Status: c.from}
		err := b.Apply(c.ev, time.Now())
		events := b.Pull()
		if c.to == "" {
			if !errors.Is(err, kernel.ErrInvalidTransition) || b.Status != c.from || len(events) != 0 {
				t.Errorf("%s --%s--> should be rejected, keep status and record nothing; got %s %v %v", c.from, c.ev, b.Status, err, events)
			}
			continue
		}
		if err != nil || b.Status != c.to {
			t.Errorf("%s --%s--> want %s, got %s (%v)", c.from, c.ev, c.to, b.Status, err)
		}
		if len(events) != 1 || events[0].(BillUpdateCompleted).Status != c.to {
			t.Errorf("%s --%s--> events = %v", c.from, c.ev, events)
		}
	}
}

func TestCaptureUsesPSPTime(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	b := Bill{Status: Authorized}
	if err := b.Apply(Capture, at); err != nil || b.CapturedAt == nil || !b.CapturedAt.Equal(at) {
		t.Fatalf("captured_at = %v, err %v; want %v", b.CapturedAt, err, at)
	}
}

func TestNewOneShot(t *testing.T) {
	ok := func(amount int64, m kernel.PaymentMethod, inst int32, tok, key string) error {
		_, err := NewOneShot(OneShotParams{
			WorkspaceID: uuid.New(), Amount: amount, PaymentMethod: m, Installments: inst, TokenID: tok, IdempotencyKey: key,
		})
		return err
	}
	if err := ok(1000, kernel.CreditCard, 0, "tok", "k"); err != nil {
		t.Fatalf("valid card charge rejected: %v", err)
	}
	if err := ok(1000, kernel.Pix, 1, "", "k"); err != nil {
		t.Fatalf("pix without token rejected: %v", err)
	}
	invalid := map[string]error{
		"no key":          ok(1000, kernel.Pix, 1, "", ""),
		"zero amount":     ok(0, kernel.Pix, 1, "", "k"),
		"unknown method":  ok(1000, "cash", 1, "", "k"),
		"22 installments": ok(1000, kernel.CreditCard, 22, "tok", "k"),
		"card w/o token":  ok(1000, kernel.CreditCard, 1, "", "k"),
	}
	for name, err := range invalid {
		if !errors.Is(err, kernel.ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
}
