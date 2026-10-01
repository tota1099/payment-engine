// Package kernel is the shared kernel: the few domain primitives every context
// speaks. Domain logic belonging to one context stays in that context.
package kernel

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Error kinds. Transport maps them to status codes; match with errors.Is.
var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrInvalid           = errors.New("invalid")
	ErrInvalidTransition = errors.New("invalid transition")
	ErrUnavailable       = errors.New("dependency unavailable")
)

// Error is a domain error with a client-safe message.
type Error struct {
	Kind error
	Msg  string
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Kind }

func Invalid(format string, a ...any) error {
	return &Error{Kind: ErrInvalid, Msg: fmt.Sprintf(format, a...)}
}

func NotFound(what string) error { return &Error{Kind: ErrNotFound, Msg: what + " not found"} }

func Unavailable(msg string) error { return &Error{Kind: ErrUnavailable, Msg: msg} }

// FSM maps event -> from -> to.
type FSM[S ~string] map[string]map[S]S

func (t FSM[S]) Next(from S, event string) (S, error) {
	if to, ok := t[event][from]; ok {
		return to, nil
	}
	return from, &Error{Kind: ErrInvalidTransition, Msg: fmt.Sprintf("cannot %s from status %s", event, from)}
}

// Tx runs fn atomically. The transaction travels in ctx, so every store
// called with that ctx joins it; nested calls reuse the outer transaction.
type Tx func(ctx context.Context, fn func(ctx context.Context) error) error

// Page is a forward keyset page: newest first, after StartingAfter.
// ponytail: forward-only cursor; add ending_before when a client needs to page back.
type Page struct {
	Limit         int32
	StartingAfter uuid.NullUUID
}

type PaymentMethod string

const (
	CreditCard PaymentMethod = "credit_card"
	Pix        PaymentMethod = "pix"
	Boleto     PaymentMethod = "boleto"
)

func (m PaymentMethod) Valid() bool { return m == CreditCard || m == Pix || m == Boleto }

type correlationKey struct{}

// WithCorrelationID tags ctx so logs and outbox events can be traced to a request.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

func CorrelationID(ctx context.Context) string { s, _ := ctx.Value(correlationKey{}).(string); return s }

// Event is a domain event: something that happened, named in the past tense.
// Its JSON form is what consumers receive.
type Event interface{ EventName() string }

// Events lets an aggregate record what happened; the use case publishes them
// in the same transaction as the write. Embed it in the aggregate.
type Events struct{ pending []Event }

func (e *Events) Record(ev Event) { e.pending = append(e.pending, ev) }

// Pull returns and clears the recorded events.
func (e *Events) Pull() []Event {
	out := e.pending
	e.pending = nil
	return out
}
