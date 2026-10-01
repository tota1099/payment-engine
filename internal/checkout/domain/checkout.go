// Package domain holds the checkout entity: a payment link that defines what
// can be paid, how, and how often. No I/O.
package domain

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/kernel"
)

type Status string

const (
	Active   Status = "active"
	Disabled Status = "disabled"
	Canceled Status = "canceled"
)

var fsm = kernel.FSM[Status]{
	"disable":    {Active: Disabled},
	"reactivate": {Disabled: Active},
	"cancel":     {Active: Canceled, Disabled: Canceled},
}

type Item struct {
	Name     string
	Amount   int64 // cents
	Quantity int
}

type Checkout struct {
	ID                uuid.UUID
	WorkspaceID       uuid.UUID
	Name              string
	Items             []Item
	PaymentMethods    []kernel.PaymentMethod
	MaxUses           *int32
	ExpiresAt         *time.Time
	ExternalReference *string
	URL               string
	Status            Status
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type NewParams struct {
	WorkspaceID       uuid.UUID
	Name              string
	Items             []Item
	PaymentMethods    []kernel.PaymentMethod
	MaxUses           *int32
	ExpiresAt         *time.Time
	ExternalReference *string
}

func New(p NewParams, now time.Time) (Checkout, error) {
	c := Checkout{
		WorkspaceID: p.WorkspaceID, Name: strings.TrimSpace(p.Name), Items: p.Items, PaymentMethods: p.PaymentMethods,
		MaxUses: p.MaxUses, ExpiresAt: p.ExpiresAt, ExternalReference: p.ExternalReference, Status: Active,
	}
	switch {
	case c.Name == "":
		return c, kernel.Invalid("name is required")
	case len(c.Items) == 0:
		return c, kernel.Invalid("items must not be empty")
	case len(c.PaymentMethods) == 0:
		return c, kernel.Invalid("payment_methods must not be empty")
	case c.MaxUses != nil && *c.MaxUses <= 0:
		return c, kernel.Invalid("max_uses must be greater than 0")
	case c.Expired(now):
		return c, kernel.Invalid("expires_at must be in the future")
	}
	for _, it := range c.Items {
		if it.Name == "" || it.Amount <= 0 || it.Quantity <= 0 {
			return c, kernel.Invalid("each item needs name, amount > 0 and quantity > 0")
		}
	}
	for _, m := range c.PaymentMethods {
		if !m.Valid() {
			return c, kernel.Invalid("unknown payment method %q", m)
		}
	}
	return c, nil
}

func (c Checkout) Expired(now time.Time) bool { return c.ExpiresAt != nil && !c.ExpiresAt.After(now) }

func (c *Checkout) Disable(now time.Time) error    { return c.applyUnexpired("disable", now) }
func (c *Checkout) Reactivate(now time.Time) error { return c.applyUnexpired("reactivate", now) }

func (c *Checkout) Cancel() (err error) {
	c.Status, err = fsm.Next(c.Status, "cancel")
	return err
}

func (c *Checkout) applyUnexpired(event string, now time.Time) (err error) {
	if c.Expired(now) {
		return kernel.Invalid("checkout is expired")
	}
	c.Status, err = fsm.Next(c.Status, event)
	return err
}

// Accepts tells whether one more payment with method fits, given the uses so far.
func (c Checkout) Accepts(method kernel.PaymentMethod, uses int64, now time.Time) error {
	switch {
	case c.Status != Active:
		return kernel.Invalid("checkout is not active")
	case c.Expired(now):
		return kernel.Invalid("checkout is expired")
	case !slices.Contains(c.PaymentMethods, method):
		return kernel.Invalid("payment_method not accepted by checkout")
	case c.MaxUses != nil && uses >= int64(*c.MaxUses):
		return kernel.Invalid("checkout reached max_uses")
	}
	return nil
}
