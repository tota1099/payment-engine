// Command api serves the payment engine's HTTP API. main.go is the
// composition root: the only place that knows every concrete type.
package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	accountpg "github.com/renanporto/payment-engine/internal/account/adapter/postgres"
	accountrest "github.com/renanporto/payment-engine/internal/account/adapter/rest"
	accountapp "github.com/renanporto/payment-engine/internal/account/app"
	billingpg "github.com/renanporto/payment-engine/internal/billing/adapter/postgres"
	billingrest "github.com/renanporto/payment-engine/internal/billing/adapter/rest"
	billingapp "github.com/renanporto/payment-engine/internal/billing/app"
	checkoutpg "github.com/renanporto/payment-engine/internal/checkout/adapter/postgres"
	checkoutrest "github.com/renanporto/payment-engine/internal/checkout/adapter/rest"
	checkoutapp "github.com/renanporto/payment-engine/internal/checkout/app"
	"github.com/renanporto/payment-engine/internal/db"
	"github.com/renanporto/payment-engine/internal/eventbus"
	"github.com/renanporto/payment-engine/internal/httpx"
	"github.com/renanporto/payment-engine/internal/psp"
	"github.com/renanporto/payment-engine/internal/psp/fake"
	webhookpg "github.com/renanporto/payment-engine/internal/webhook/adapter/postgres"
	webhookrest "github.com/renanporto/payment-engine/internal/webhook/adapter/rest"
	webhookapp "github.com/renanporto/payment-engine/internal/webhook/app"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, env("DATABASE_URL", "postgres://payment:payment@localhost:5432/payment_engine"))
	if err != nil {
		slog.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	jobs, err := eventbus.NewClient(pool, false) // insert-only; cmd/worker processes
	if err != nil {
		slog.Error("river client", "err", err)
		os.Exit(1)
	}

	provider := fake.New(env("FAKE_PSP_SECRET", "dev-secret"))
	srv := &http.Server{
		Addr:              env("ADDR", ":8080"),
		Handler:           newApp(pool, provider, eventbus.Publisher{Client: jobs}, devRoutes()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

// docs is the Swagger UI and its OpenAPI spec, served at /docs/. Production
// puts it behind the proxy's basic auth.
//
//go:embed docs
var docs embed.FS

// devRoutes enables the fake-PSP simulator outside production, or in
// production while the fake PSP is the only one (ENABLE_FAKE_PSP_EVENTS=true).
func devRoutes() bool {
	return os.Getenv("APP_ENV") != "production" || os.Getenv("ENABLE_FAKE_PSP_EVENTS") == "true"
}

// newApp wires use cases to their adapters and mounts each context's routes.
func newApp(pool *pgxpool.Pool, provider *fake.Provider, events eventbus.Publisher, devRoutes bool) http.Handler {
	d := db.DB{Pool: pool}

	accounts := &accountapp.AccountInteractor{Repo: accountpg.Accounts{DB: d}, Tx: d.InTx}
	workspaces := &accountapp.WorkspaceInteractor{
		Accounts: accountpg.Accounts{DB: d}, Repo: accountpg.Workspaces{DB: d}, Links: accountpg.ConnectedAccounts{DB: d},
		PSP: provider, Events: events, Tx: d.InTx,
	}
	checkouts := &checkoutapp.Interactor{
		Workspaces: checkoutpg.Workspaces{DB: d}, Repo: checkoutpg.Checkouts{DB: d}, Usage: checkoutpg.Usage{DB: d},
		Links: checkoutpg.PaymentLinks{DB: d}, PSP: provider, Tx: d.InTx,
	}
	payments := &billingapp.PaymentInteractor{
		Workspaces: billingpg.Workspaces{DB: d}, Checkouts: checkouts, Bills: billingpg.Bills{DB: d},
		Ledger: billingpg.Ledger{DB: d}, Charges: billingpg.Charges{DB: d}, PSP: provider, Events: events, Tx: d.InTx,
	}
	billQueries := &billingapp.QueryInteractor{
		Workspaces: billingpg.Workspaces{DB: d}, Checkouts: billingpg.Checkouts{DB: d},
		Bills: billingpg.Bills{DB: d}, Ledger: billingpg.Ledger{DB: d}, Provider: provider.Name(),
	}
	ingest := &webhookapp.IngestInteractor{
		Inbox: webhookpg.Inbox{DB: d}, Refs: webhookpg.Refs{DB: d}, Tx: d.InTx,
		Routes: map[string]webhookapp.Apply{
			"workspace": workspaces.ApplyOnboardingEvent,
			"bill":      payments.ApplyEvent,
		},
	}

	accountHTTP := &accountrest.Handler{Accounts: accounts, Workspaces: workspaces}
	webhookHTTP := &webhookrest.Handler{PSP: provider, Ingest: ingest}
	account := httpx.AccountGuard(accountHTTP.Sources)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db down"})
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	accountHTTP.Routes(mux, httpx.ConsumerGuard, account)
	(&checkoutrest.Handler{Checkouts: checkouts}).Routes(mux, account)
	(&billingrest.Handler{Payments: payments, Queries: billQueries}).Routes(mux, account)
	webhookHTTP.Routes(mux)
	mux.Handle("GET /docs/", http.FileServerFS(docs))
	if devRoutes {
		mux.HandleFunc("POST /dev/fake-psp/events", fakeEvent(provider, mux))
	}
	return httpx.Base(mux)
}

// fakeEvent plays the PSP: signs {type, external_id, code} and delivers it
// through the real webhook route.
func fakeEvent(p *fake.Provider, mux http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ev psp.Event
		if err := httpx.Decode(r, &ev); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if ev.ID == "" {
			ev.ID = "evt_" + uuid.NewString()
		}
		ev.OccurredAt = time.Now().UTC()
		body, _ := json.Marshal(ev)
		req := httptest.NewRequestWithContext(r.Context(), http.MethodPost, "/api/v1/webhooks", bytes.NewReader(body))
		req.Header.Set(fake.SignatureHeader, p.SignatureFor(body, time.Now()))
		mux.ServeHTTP(w, req)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
