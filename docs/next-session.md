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

## F2 plan: wallet, void, client notifications, mutation requests, idempotency

Ship it as five slices, each a commit (or a small branch) with tests. Follow the recipes in `docs/architecture.md`. Reference behaviour lives in `~/Projects/totvs-pay`.

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
- **Partial AND total refunds are in scope** (user decision, 2026-10-01). Several void requests may be in flight on one bill.
- **Refundable balance:** captured amount minus resolved voids, minus unresolved `void_requested`. A request above the balance gets 422.
- **Domain:**
  - `Bill.RequestVoid(amount)` checks the balance; omitting `amount` means "the whole balance".
  - `Bill.Apply(Void, at)` already covers pending/authorized.
  - A captured bill stays `captured` while partially refunded and becomes `voided` when the balance reaches 0. No new status; expose `refunded_amount` and `refundable_amount` in the API. Extend the FSM and its tests.
- **PSP port:** `Void(ctx, idempotencyKey, externalID, amount) (VoidResult, error)`. The fake decides by token or amount.
- **API:** `POST /api/v1/workspaces/{id}/bills/{bid}/void` (Idempotency-Key).
- **Webhooks:** `bill.voided` and `bill.void_rejected` close A.
- **Done when:** sync accept, sync reject, async accept by webhook, a duplicate void request and an over-balance request are all covered in tests.

### 3. Client notifications (finish the outbox) — PROPOSED, awaiting user approval
- **Endpoints belong to the consumer (source), not to the account.** The source is the integrator; accounts are its customers. A per-account override waits until someone asks for it.
  - New table `webhook_endpoints(id, source, url, secret, event_types[], active)`.
  - API `POST/GET/DELETE /api/v1/webhook_endpoints`, behind the consumer guard. The secret is shown only on create.
- **Two-stage fan-out:**
  - The current `NotifyArgs` job becomes *dispatch*: it resolves event → workspace → account → sources → subscribed endpoints, then inserts one *deliver* job per endpoint. Each endpoint has its own retry, so one dead consumer never blocks the others.
  - Domain events must carry `workspace_id`; add it to `BillUpdateCompleted`.
- **Envelope, Stripe-like:** `{id, type, created_at, account_id, workspace_id, data}`. Consumers dedupe by `id`. Ordering is not guaranteed: `data` carries the current status, and consumers re-fetch if needed.
- **Signature:** `Payment-Engine-Signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "t.body")>`, the same scheme as `psp/fake`.
- **Delivery:** non-2xx returns an error, so River retries. `MaxAttempts` is about 12 with exponential backoff, roughly one day. A discarded job is recorded as a failed delivery. Auto-disabling endpoints comes later.
- **Done when:** an `httptest.Server` consumer receives a signed delivery, a failing consumer is retried without delaying a healthy one, and an endpoint not subscribed to an event type receives nothing.

### 4. Workspace mutation requests (async)
- **Reference:** totvs-pay `account_management` `mutation_requests`, `RequestUpdate`/`RequestDisabling`/`RequestReactivation`, and `docs/outbox-events.md`.
- **API:**
  - `PATCH /api/v1/workspaces/{id}`, `POST …/disable` and `POST …/reactivate` each create a `mutation_request` (pending) and insert a River job **in the same Tx**, then return 202.
  - `GET …/mutation_requests` lists them.
- **Job workers live in the context:** add an `adapter/jobs` layer (a driver, like `adapter/rest`). Update `internal/arch` to allow it, with the same import rules as rest but `river` instead of `httpx`.
- **Worker registration:** `eventbus.Workers()` becomes `Workers(extra ...func(*river.Workers))`, so `cmd/worker` registers each context's workers.
- **PSP port:** `UpdateConnectedAccount`, `DisableConnectedAccount`, `ReactivateConnectedAccount`.
- **Events:** `workspace_update_completed|failed`, `workspace_disabling_completed|failed`, `workspace_reactivation_completed|failed`.

### 5. Idempotency middleware — PROPOSED, awaiting user approval
**Hybrid design.** A generic HTTP middleware replays responses for any POST/PATCH. Domain-level uniqueness stays for money: bills, voids and payment profiles, where the key also feeds the ledger and the PSP. The domain layer is needed because the use case commits before the response is stored. A crash between the two re-runs the use case on retry, and only domain uniqueness prevents a double charge.

- **Where:** `httpx`, with its own storage port, implemented in Postgres.
- **Table:** `idempotency_keys(source, key, method, path, request_hash, state, response_status, response_body, created_at)`, PK `(source, key)`.
- **New key:** insert `in_progress`, run the handler, store the response.
  - 2xx and 4xx responses are stored.
  - On 5xx, delete the row so a retry re-executes; the use cases are idempotent.
- **Seen key:**
  - Completed with the same request hash: replay the stored response with `Idempotent-Replayed: true`.
  - Different hash: 422.
  - Still `in_progress`: 409.
- **Requirement:** **required** on money routes (bills, void, payment profiles). **Optional but honoured** on create account, workspace and checkout.
- Update the `AGENTS.md` invariant to say exactly this.
- **Cleanup:** a River periodic job deletes keys older than 24h.
- **Done when:** a replay returns a byte-identical response, a concurrent duplicate gets 409, a changed body gets 422, and a 5xx leaves the key reusable.

## Known debt (pick up when relevant)
- **Pagination** is forward-only (`starting_after`). totvs-pay has bidirectional keyset pagination (its ADR-009).
- **Webhooks** are processed synchronously (D-004). Revisit if PSP timeouts appear.
- **Migrations:** the goose CLI was not verified against `db/migrations/migrations.go`. Always use `go run ./cmd/migrate`.
- **Missing ops pieces:** no CI, Dockerfile, golangci-lint config or metrics yet. Natural candidates once F2 lands.
- **Insights (F4)** must be computed from our own data. The fake PSP has no settlement data for receivables or conciliations.

## Decisions and open items
1. **Refunds:** partial AND total. Decided by the user on 2026-10-01; see slice 2.
2. **Notification endpoints:** proposal in slice 3. **Ask the user to approve or adjust before building.**
3. **Idempotency middleware:** proposal in slice 5. **Ask the user to approve or adjust before building.** If approved, build it first in F2, so the new routes are born with it.
