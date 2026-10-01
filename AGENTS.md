# payment-engine

A Go payment engine that reaches feature parity with `~/Projects/totvs-pay` (Rails) in phases. It talks to PSPs through `internal/psp`: `fake` today, Stripe next.

## Commands

Commands to run, migrate and test are in `README.md` → Run. After changing `db/migrations` or `db/queries`, run `go tool sqlc generate`. Before finishing any change, `go vet ./... && go test ./...` must pass.

## Read before acting

- **Session handoff** (`docs/next-session.md`): read first when starting a session. It holds the current branch state, how to resume and the detailed next steps.

- **Architecture** (`docs/architecture.md`): read before adding or moving a use case, context, port, PSP adapter or webhook event. It holds the layout, the dependency rule and step-by-step recipes.
- **Domain language** (`docs/domain.md`): read before naming a type, status, field or endpoint.
- **Decisions** (`docs/decisions.md`): read before changing stack, PSP shape, webhook flow or layering. Append a new entry when you make such a change.
- **Roadmap** (`docs/roadmap.md`): read when picking the next phase. Tick a phase off when it is done.
- **Reference behaviour**: when a feature's rules are unclear, read the matching totvs-pay context in `~/Projects/totvs-pay/app/contexts/<context>/` and `docs/adr/`.

## Invariants

- **Clean layers:** each context has `domain` ← `app` ← `adapter/{postgres,rest}`, and dependencies point inward. Only `cmd/*` knows concrete types. `internal/arch` enforces this.
- **Contexts stay sealed:** to reach another context, declare an output port in your `app` and wire it in `cmd/api`.
- **Domain events:** aggregates record them, and interactors publish them inside their `Tx` (River jobs, never outside a transaction).
- **Money** is `int64` cents plus a currency.
- **Idempotent writes:** every mutating API call and every PSP call carries an idempotency key. A retry returns the original result.
- **PSP calls run outside DB transactions.** The result is then applied in one transaction that locks the aggregate and publishes its events.
- **PSP is the source of truth** for money state and timestamps. Webhooks are verified (signature + replay window) and deduplicated before they are applied.
- **Card data:** handle only provider tokens (`token_id`). Logs stay free of tokens, secrets and documents.
- **Tests:** aggregates get table-driven transition tests. Use cases get tests against real Postgres. Each bug fix comes with a test that fails without the fix.
