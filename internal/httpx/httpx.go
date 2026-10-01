// Package httpx holds the transport glue shared by every context: JSON I/O,
// domain-error mapping, consumer/account authentication and pagination.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/renanporto/payment-engine/internal/kernel"
)

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errorJSON(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

var statusByKind = []struct {
	kind   error
	status int
}{
	{kernel.ErrInvalid, http.StatusUnprocessableEntity},
	{kernel.ErrInvalidTransition, http.StatusUnprocessableEntity},
	{kernel.ErrNotFound, http.StatusNotFound},
	{kernel.ErrConflict, http.StatusConflict},
	{kernel.ErrUnavailable, http.StatusBadGateway},
}

// Fail maps a domain error to a response. Anything unmapped is logged and hidden.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	for _, m := range statusByKind {
		if errors.Is(err, m.kind) {
			errorJSON(w, m.status, err.Error())
			return
		}
	}
	if pe := (*pgconn.PgError)(nil); errors.As(err, &pe) && pe.Code == "23505" {
		errorJSON(w, http.StatusConflict, "already exists")
		return
	}
	slog.ErrorContext(r.Context(), "request failed", "err", err, "path", r.URL.Path, "request_id", kernel.CorrelationID(r.Context()))
	errorJSON(w, http.StatusInternalServerError, "internal error")
}

func Decode(r *http.Request, v any) error {
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v); err != nil {
		return kernel.Invalid("invalid JSON body: %v", err)
	}
	return nil
}

func PathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, kernel.NotFound(name)
	}
	return id, nil
}

type ctxKey int

const (
	keySource ctxKey = iota
	keyAccount
)

func Source(ctx context.Context) string { s, _ := ctx.Value(keySource).(string); return s }

// AccountID is the authenticated X-Account-Id; set by RequireAccount.
func AccountID(ctx context.Context) uuid.UUID { id, _ := ctx.Value(keyAccount).(uuid.UUID); return id }

// Base tags the request with a correlation id and reads the consumer
// (X-Consumer-Username, set by the API gateway) as the request source.
func Base(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := kernel.WithCorrelationID(r.Context(), id)
		if src := r.Header.Get("X-Consumer-Username"); src != "" {
			ctx = context.WithValue(ctx, keySource, src)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequireConsumer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if Source(r.Context()) == "" {
			errorJSON(w, http.StatusForbidden, "invalid consumer")
			return
		}
		next(w, r)
	}
}

// SourcesOf returns the consumers that own an account.
type SourcesOf func(ctx context.Context, accountID uuid.UUID) ([]string, error)

// RequireAccount authenticates X-Account-Id and checks the consumer owns it.
func RequireAccount(sources SourcesOf) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return RequireConsumer(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			id, err := uuid.Parse(r.Header.Get("X-Account-Id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, "account not found")
				return
			}
			owners, err := sources(ctx, id)
			if err != nil {
				Fail(w, r, err)
				return
			}
			if !slices.Contains(owners, Source(ctx)) {
				slog.WarnContext(ctx, "account_ownership_violation", "consumer", Source(ctx), "account_id", id, "path", r.URL.Path)
				errorJSON(w, http.StatusForbidden, "access denied")
				return
			}
			next(w, r.WithContext(context.WithValue(ctx, keyAccount, id)))
		})
	}
}

func ParsePage(r *http.Request) (kernel.Page, error) {
	p := kernel.Page{Limit: 20}
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			return p, kernel.Invalid("limit must be between 1 and 100")
		}
		p.Limit = int32(n)
	}
	if s := r.URL.Query().Get("starting_after"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			return p, kernel.Invalid("invalid starting_after")
		}
		p.StartingAfter = uuid.NullUUID{UUID: id, Valid: true}
	}
	return p, nil
}

// List serves a keyset page: parses ?limit&starting_after, fetches one extra
// row to know whether a next page exists, and writes {data, next_cursor}.
func List[T, R any](w http.ResponseWriter, r *http.Request, fetch func(kernel.Page) ([]T, error), id func(T) uuid.UUID, present func(T) R) error {
	p, err := ParsePage(r)
	if err != nil {
		return err
	}
	want := p.Limit
	p.Limit++
	rows, err := fetch(p)
	if err != nil {
		return err
	}
	resp := struct {
		Data       []R     `json:"data"`
		NextCursor *string `json:"next_cursor"`
	}{Data: []R{}}
	if len(rows) > int(want) {
		rows = rows[:want]
		c := id(rows[len(rows)-1]).String()
		resp.NextCursor = &c
	}
	for _, row := range rows {
		resp.Data = append(resp.Data, present(row))
	}
	JSON(w, http.StatusOK, resp)
	return nil
}

// HandlerFunc is a handler that returns its error instead of writing it.
type HandlerFunc func(http.ResponseWriter, *http.Request) error

// Guard turns a HandlerFunc into an authenticated http.HandlerFunc.
type Guard func(HandlerFunc) http.HandlerFunc

// Handle writes the returned error with Fail.
func Handle(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			Fail(w, r, err)
		}
	}
}

// ConsumerGuard requires X-Consumer-Username only.
func ConsumerGuard(fn HandlerFunc) http.HandlerFunc { return RequireConsumer(Handle(fn)) }

// AccountGuard requires a consumer that owns X-Account-Id.
func AccountGuard(sources SourcesOf) Guard {
	require := RequireAccount(sources)
	return func(fn HandlerFunc) http.HandlerFunc { return require(Handle(fn)) }
}
