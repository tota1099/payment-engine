# Decisions

Append-only. Each entry records the context, the decision and the cost. When you reverse a decision, add a new entry that supersedes the old one; old entries are never edited.

## D-001 Stack: stdlib net/http + pgx + sqlc + goose (2026-10-01)
**Context:** a personal project, so the dependency count should stay small.
**Decision:**
- Routing uses the Go 1.22+ `ServeMux` patterns.
- SQL is hand-written and compiled by sqlc.
- goose and sqlc are pinned as `go tool` deps in `go.mod`.
- Postgres 18 ids come from `uuidv7()`, so they are time-ordered and work as keyset cursors.

**Cost:** no ORM conveniences. Every query is written by hand.

## D-002 PSP behind a Stripe-shaped port, fake first (2026-10-01)
**Context:** there are no PSP credentials yet. Stripe is the planned real provider.
**Decision:**
- `psp.Provider` speaks Stripe's model: connected account, payment link, PaymentIntent-like charge, int64 cents, idempotency keys, and a `t=,v1=` HMAC signature with a replay window.
- `psp/fake` is deterministic and drives dev and tests.

**Cost:** a non-Stripe PSP (Malga, Pagar.me) may need its own mapping inside its adapter.

## D-003 DDD/SOLID the Go way (2026-10-01)
**Context:** totvs-pay uses DDD bounded contexts with use cases, contracts and presenters on Rails.
**Decision:**
- A context is one flat package, with files split by role: domain, service, postgres, http.
- Ports are interfaces declared by the consumer.
- No per-layer directories and no generic repository.
- The rules live in `docs/architecture.md`.

**Cost:** the boundaries rely on review and import checks, not the compiler.

## D-004 Webhooks processed synchronously in the request (2026-10-01)
**Context:** totvs-pay processes webhooks in a Sidekiq job.
**Decision:**
- Verify, dedupe and apply the event in a single DB transaction during the request.
- Ack business rejections with 200.
- Return 404 for unknown objects so the PSP retries.

**Cost:** slow handlers delay the PSP's request. Move to async (store, ack, process from the queue) if p99 latency or PSP timeouts require it.

## D-005 Transaction travels in ctx (2026-10-01)
**Context:** a webhook must dedupe the event and apply it atomically, but the apply step belongs to another context's service, which must not import `db`.
**Decision:**
- Services receive `kernel.Tx` and run their writes inside it.
- `db.DB` stores the `pgx.Tx` in `ctx` and binds `Q(ctx)` to it.
- A nested `InTx` joins the outer transaction.

**Cost:** the transaction is implicit. A store called with a `ctx` from outside the `Tx` callback runs on the pool, so always use the `ctx` the callback receives.

## D-006 Clean Architecture layers as packages (2026-10-01)
**Context:** the role-based files from D-003 relied on a test for layering. The team prefers explicit layers.
**Supersedes:** the "one flat package" part of D-003.
**Decision:**
- Each context has the packages `domain`, `app` (input and output ports + interactors), `adapter/postgres` and `adapter/rest`.
- The webhook flow became a use case (`webhook/app.IngestInteractor`).
- Output ports are split by role (ISP).
- Each REST adapter mounts its own routes.
- Aggregates record domain events.

**Cost:** about twice the files, import aliases in `cmd/api`, and mapping code between layers.

## D-007 River replaces the hand-rolled outbox (2026-10-01)
**Context:** F2 needed an outbox worker with `SKIP LOCKED`, retry, backoff and dead-lettering.
**Decision:**
- Domain events become River jobs, inserted with `InsertTx` in the business transaction.
- River provides retry/backoff, discard after `MaxAttempts`, scheduling, uniqueness and an optional UI.
- `outbox_events` was dropped.
- Migrations run through `db/migrations.Up` (goose, then rivermigrate).

**Cost:** a dependency on River and its tables. Alternatives considered: our own worker with LISTEN/NOTIFY (more code), `pg_logical_emit_message` with logical replication (harder to operate), Debezium with Kafka (heavy infra).
