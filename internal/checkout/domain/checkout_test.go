package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/kernel"
)

var now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func valid(t *testing.T, maxUses *int32, expiresAt *time.Time) Checkout {
	t.Helper()
	c, err := New(NewParams{
		WorkspaceID: uuid.New(), Name: "Plan", Items: []Item{{Name: "x", Amount: 100, Quantity: 1}},
		PaymentMethods: []kernel.PaymentMethod{kernel.Pix}, MaxUses: maxUses, ExpiresAt: expiresAt,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAccepts(t *testing.T) {
	c := valid(t, ptr(int32(2)), ptr(now.Add(time.Hour)))
	cases := map[string]struct {
		c      Checkout
		method kernel.PaymentMethod
		uses   int64
		at     time.Time
		ok     bool
	}{
		"fits":           {c, kernel.Pix, 1, now, true},
		"max_uses":       {c, kernel.Pix, 2, now, false},
		"method":         {c, kernel.CreditCard, 0, now, false},
		"expired":        {c, kernel.Pix, 0, now.Add(2 * time.Hour), false},
		"disabled":       {Checkout{Status: Disabled, PaymentMethods: c.PaymentMethods}, kernel.Pix, 0, now, false},
		"unlimited fits": {valid(t, nil, nil), kernel.Pix, 1 << 40, now, true},
	}
	for name, tc := range cases {
		err := tc.c.Accepts(tc.method, tc.uses, tc.at)
		if tc.ok != (err == nil) {
			t.Errorf("%s: ok=%v, err=%v", name, tc.ok, err)
		}
	}
}

func TestTransitions(t *testing.T) {
	c := valid(t, nil, ptr(now.Add(time.Hour)))
	if err := c.Disable(now); err != nil || c.Status != Disabled {
		t.Fatalf("disable: %s %v", c.Status, err)
	}
	if err := c.Reactivate(now.Add(2 * time.Hour)); !errors.Is(err, kernel.ErrInvalid) {
		t.Fatalf("reactivating an expired checkout: want ErrInvalid, got %v", err)
	}
	if err := c.Cancel(); err != nil || c.Status != Canceled {
		t.Fatalf("cancel: %s %v", c.Status, err)
	}
	if err := c.Reactivate(now); !errors.Is(err, kernel.ErrInvalidTransition) {
		t.Fatalf("reactivating a canceled checkout: want ErrInvalidTransition, got %v", err)
	}
}
