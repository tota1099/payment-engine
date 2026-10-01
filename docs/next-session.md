# Next session — handoff

Read this first, then `AGENTS.md`. Update or delete this file when the work it describes is done; the roadmap (`docs/roadmap.md`) is the durable record.

## Where we are (2026-10-01)

- **Branches**
  - `main`: bootstrap only (tooling, harness, docs).
  - `feat/f1-core`: the whole implementation (F1 + F1.5 + F1.6) in layer-by-layer commits, every commit builds. Not merged yet. There is no git remote.
- **Done**
  - **F1, core:** accounts, workspaces, checkouts, one-shot bills, ledger, signed and idempotent webhooks.
  - **F1.5, DDD alignment:** kernel, aggregates, sealed contexts, tests.
  - **F1.6, Clean Architecture:** layers as packages and an ingest use case for webhooks; River replaces the outbox.
- **Proven by tests** (`go test ./...`)
  - Domain transition tables.
  - Concurrent same-key idempotency.
  - The `max_uses` race.
  - Webhook dedupe, ignore and retry.
  - Events roll back with the business transaction.
  - Worker drains River jobs.
  - Layer and import rules.

  The concurrency and layer tests were mutation-checked: each failed when the protection was removed.
- **Decisions so far:** `docs/decisions.md`, D-001 … D-007.

## Resume

```sh
git switch feat/f1-core
docker compose up -d postgres
go run ./cmd/migrate
go vet ./... && go test ./...
```

Before starting F2, ask the user to review `feat/f1-core` and merge it into `main` (fast-forward). Then branch `feat/f2-wallet-void-notifications` from `main`.

## F2 plan: wallet, void, client notifications, mutation requests

Ship it as four slices, each a commit (or a small branch) with tests. Follow the recipes in `docs/architecture.md`. Reference behaviour lives in `~/Projects/totvs-pay`.

### 1. Customer wallet: new context `internal/wallet`
- **Reference:** totvs-pay `app/contexts/customer_wallet/`, the `payment_profiles` table.
- **Domain:** `PaymentProfile` (workspace, payment type, cardholder document, status, created from a provider token).
- **PSP port:** `SavePaymentMethod(ctx, idempotencyKey, connectedAccount, token) (externalID, error)`. In Stripe terms: a Customer plus a PaymentMethod attached to it. Implement it in `psp/fake`.
- **API:** `POST /api/v1/workspaces/{id}/payment_profiles` (Idempotency-Key) and `GET /api/v1/workspaces/{id}/payment_profiles` (paginated).
- **Billing integration:**
  - One-shot accepts `payment_profile_id` **xor** `token_id`, same as the totvs-pay contract. Validate this in `domain.NewOneShot`.
  - Billing reads the profile through a new output port `billing/app.PaymentProfiles`, implemented by a wallet input port and wired in `cmd/api`.
  - `transactions.payment_profile_id` gets a new migration.
- **Done when:** a one-shot with a saved profile charges, an E2E test covers it, and the arch test passes for the new context.

### 2. Bill void (refund)
- **Read first:** totvs-pay ADR-017 (classifying the PSP result) and ADR-018 (`void_rejected`).
- **Ledger model:**
  - The request is transaction A (`void_requested`, keyed by the client's Idempotency-Key).
  - The outcome is transaction B (`voided` or `void_rejected`), which links to A.
  - A sync rejection appends B; it never mutates A. At most one rejection per request: look up B by its link to A before inserting.
- **Refundable balance:** captured amount minus resolved voids, minus unresolved `void_requested`. Decide whether partial refunds are in scope. totvs-pay supports them, so ask the user.
- **Domain:**
  - `Bill.RequestVoid(amount)` checks the balance.
  - `Bill.Apply(Void, at)` already covers pending/authorized. A captured bill needs a refund path: `captured → voided` when fully refunded. Extend the FSM and its tests.
- **PSP port:** `Void(ctx, idempotencyKey, externalID, amount) (VoidResult, error)`. The fake decides by token or amount.
- **API:** `POST /api/v1/workspaces/{id}/bills/{bid}/void` (Idempotency-Key).
- **Webhooks:** `bill.voided` and `bill.void_rejected` close A.
- **Done when:** sync accept, sync reject, async accept by webhook, a duplicate void request and an over-balance request are all covered in tests.

### 3. Client notifications (finish the outbox)
- **Endpoints:** consumers register `(source, url, secret)` in a new table. An admin endpoint or seed is enough for now.
- **Worker:** turn `eventbus.NotifyWorker` into an HTTP POST to the source's URL.
  - Sign it like the PSPs do: `X-Signature: t=,v1=` HMAC.
  - Return an error on non-2xx, so River retries with backoff.
  - Set `MaxAttempts` to about 10.
  - When the job is discarded, record a `*_failed` notification.
- **Routing:** decide whether the target source comes from the event (the account's sources) or is resolved by the worker. Prefer resolving in the worker through a port.
- **Done when:** a test with an `httptest.Server` consumer receives a signed delivery, and a failing consumer causes a retry.

### 4. Workspace mutation requests (async)
- **Reference:** totvs-pay `account_management` `mutation_requests`, `RequestUpdate`/`RequestDisabling`/`RequestReactivation`, and `docs/outbox-events.md`.
- **API:**
  - `PATCH /api/v1/workspaces/{id}`, `POST …/disable` and `POST …/reactivate` each create a `mutation_request` (pending) and insert a River job **in the same Tx**, then return 202.
  - `GET …/mutation_requests` lists them.
- **Job workers live in the context:** add an `adapter/jobs` layer (a driver, like `adapter/rest`). Update `internal/arch` to allow it, with the same import rules as rest but `river` instead of `httpx`.
- **Worker registration:** `eventbus.Workers()` becomes `Workers(extra ...func(*river.Workers))`, so `cmd/worker` registers each context's workers.
- **PSP port:** `UpdateConnectedAccount`, `DisableConnectedAccount`, `ReactivateConnectedAccount`.
- **Events:** `workspace_update_completed|failed`, `workspace_disabling_completed|failed`, `workspace_reactivation_completed|failed`.

## Known debt (pick up when relevant)
- **Idempotency gap:** `AGENTS.md` says every mutating API call takes an Idempotency-Key, but create account, workspace and checkout don't yet.
  - Proposal: a generic idempotency middleware storing `(source, key) → response` in Postgres, or enforce it per use case.
  - Discuss with the user before building.
- **Pagination** is forward-only (`starting_after`). totvs-pay has bidirectional keyset pagination (its ADR-009).
- **Webhooks** are processed synchronously (D-004). Revisit if PSP timeouts appear.
- **Migrations:** the goose CLI was not verified against `db/migrations/migrations.go`. Always use `go run ./cmd/migrate`.
- **Missing ops pieces:** no CI, Dockerfile, golangci-lint config or metrics yet. Natural candidates once F2 lands.
- **Insights (F4)** must be computed from our own data. The fake PSP has no settlement data for receivables or conciliations.

## Open questions for the user
1. Partial refunds in F2, or full void only?
2. Should client notification endpoints be per source (consumer) or per account?
3. Should we add the generic idempotency middleware now (it touches every mutating route) or later?
