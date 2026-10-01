# Architecture

Go modular monolith following **Clean Architecture** inside **DDD bounded contexts**. Each context splits into layers, and dependencies point inward:

```
adapter/rest ─┐
              ├─▶ app (use cases + ports) ─▶ domain (entities)
adapter/postgres ┘
```

`cmd/*` is the composition root: the only code that knows every concrete type. `internal/arch/arch_test.go` enforces the layers, so `go test` fails on a violation.

## Context layout

`internal/<context>/` holds:

| Layer | Package | Holds | May import (besides stdlib) |
|---|---|---|---|
| Entities | `domain` | Aggregates, value objects, typed statuses, transition methods (`bill.Apply(Capture, at)`), **domain events** recorded with `kernel.Events`. No I/O | `uuid`, `kernel` |
| Use cases | `app` | `ports.go` declares the **input ports** (interfaces drivers call: `Payments`, `Workspaces`) and **output ports** (what the use cases need: `Bills`, `Ledger`, `Acquirer`, `EventPublisher`), one role per interface. Interactors (`PaymentInteractor`) implement the input ports | + `psp` (neutral vocabulary), own `domain` |
| Adapter (driven) | `adapter/postgres` | One struct per output port, mapping sqlc rows ↔ entities | + `db`, `pgx`, own `domain`/`app` |
| Adapter (driver) | `adapter/rest` | Handler holding input-port interfaces, request decoding, presenters (response structs), `Routes(mux, guards…)` | + `httpx`, own `domain`/`app` |

Domain and app never import `net/http` or `database/sql`. Interactors return entities, and only presenters turn them into JSON. Every adapter asserts what it implements with `var _ app.Port = Impl{}`.

A context may skip a layer it does not need. `webhook` has no `domain`: its use case only routes events to other contexts.

## Shared packages (infra)

- `internal/kernel`: the shared kernel. It is a leaf: stdlib and `uuid` only, and no logic that belongs to a single context.
  - Error kinds: `ErrInvalid`, `ErrInvalidTransition`, `ErrNotFound`, `ErrConflict`, `ErrUnavailable`.
  - `FSM[S]` for transition tables.
  - `Tx`, the transaction port.
  - `Event` and `Events`, for domain events.
  - `Page`, `PaymentMethod`, and the correlation id.
- `internal/db`: sqlc output plus `db.DB`.
  - `Q(ctx)` returns queries bound to the `ctx` transaction.
  - `InTx` implements `kernel.Tx` and joins an outer transaction if one exists.
  - `TxFrom(ctx)` returns the transaction in `ctx`; `NotFound(err, what)` maps a missing row to `ErrNotFound`.
  - Query files are split per context (`db/queries/<context>.sql`), and query names start with the owning context (`BillingInsert`).
- `internal/httpx`: transport glue.
  - Maps kernel errors to status codes: Invalid and InvalidTransition → 422, NotFound → 404, Conflict → 409, Unavailable → 502, anything else → 500 with a log line.
  - `ConsumerGuard` and `AccountGuard` for auth; `List` for keyset pages; `HandlerFunc` handlers return an `error`.
- `internal/psp`: the provider port plus its neutral vocabulary. Adapters live in `psp/<name>`. Only `psp/*` knows provider vocabulary.
- `internal/eventbus`: implements every context's `EventPublisher` with River.
  - `Publish` inserts a job **in the caller's transaction** (`ErrNoTransaction` outside one). This is the transactional outbox.
  - `cmd/worker` runs the workers.
- `internal/testdb`: `testdb.New(t)` gives each test a fresh schema migrated by `db/migrations.Up`.

## Crossing contexts

No layer of one context imports another context.

- **Read another context's data:** add an output port plus a read model in `app` (`billing/app.Workspace`). Implement it in `adapter/postgres` with a context-prefixed query that selects only the columns needed.
- **Ask another context for a decision synchronously:** declare an output port in your `app` (`billing/app.CheckoutReserver`). The other context exposes a matching input port (`checkout/app.Reserver`), and `cmd/api` passes one interactor as the other's dependency.
- **Tell other systems something happened:** the aggregate records a domain event, and the interactor publishes it inside its `Tx`.
- **PSP events:** `webhook/app.IngestInteractor` dedupes them and routes them by `provider_refs.entity_type` to the owning context's input port (`Routes` in `cmd/api`).

## Mutating use case

1. Build or validate through the domain (`domain.NewOneShot`, `domain.NewDocument`). This produces `ErrInvalid`.
2. Load through output ports. Fail fast if the transition is not allowed: try it on a copy of the entity.
3. Call the PSP **outside** any transaction, with an idempotency key derived from our id. Retrying the call must be safe.
4. In **one** `uc.Tx(ctx, …)`:
   - lock the aggregate (`GetForUpdate`);
   - apply the domain method;
   - save;
   - `Events.Publish(ctx, agg.Pull()...)`.

   Every port called with that `ctx` joins the same transaction.

The PSP is the source of truth for money state. Webhooks win over synchronous responses, and timestamps such as `captured_at` come from the PSP event.

## Recipes

**New use case:**
1. Add the domain method, with a table test.
2. Add an input-port method in `app/ports.go`, then implement it in the interactor. Add or extend an output port if needed.
3. Implement the port in `adapter/postgres`, with a query in `db/queries/<context>.sql` → `go tool sqlc generate`.
4. Add the handler and its route in `adapter/rest`.
5. Wire new dependencies in `cmd/api/main.go`.
6. Cover the flow in `cmd/api/app_test.go`.

**New context:**
1. Create `internal/<name>/{domain,app,adapter/postgres,adapter/rest}`.
2. Wire it in `cmd/api`.
3. The arch test picks it up automatically.

**New domain event:**
1. Add a struct with `EventName()` in `domain`, recorded by the aggregate method that causes it.
2. The interactor publishes what `Pull()` returns.
3. Consumers receive it through `eventbus.NotifyWorker`.

**New background job:**
1. Add an args struct with `Kind()` and a worker in `internal/eventbus`, or in the owning context's adapter if it needs that context's ports.
2. Register it in `eventbus.Workers()`.
3. Insert it with `InsertTx` inside the use case's `Tx`.

**New PSP adapter:**
1. Create `internal/psp/<name>` implementing `psp.Provider`.
2. Map its events to the `psp.Event*` types.
3. Verify the signature and replay window in `ParseWebhook`.
4. Make every mutating call idempotent.
5. Pick it in `cmd/api`.
6. Port the `psp/fake` test cases to the new adapter.

**New webhook event:**
1. Add a `psp.Event*` constant and map it in each adapter.
2. Handle it in the owning interactor's apply method. For a new entity type, add a route in `cmd/api`.
3. Business rejections are acked with 200; unknown objects return 404 so the PSP retries.
