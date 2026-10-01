# Roadmap

Feature parity target: `~/Projects/totvs-pay`. Each phase ends with `go test ./...` passing, with its flow covered end to end in `cmd/api/app_test.go`.

- [x] **F1 Core:** accounts, workspaces (accredit via PSP, activation by webhook), checkouts, one-shot bills, transactions, signed and idempotent webhooks, outbox writes.
- [x] **F1.5 Align with `docs/architecture.md`:**
  - Add `internal/kernel` (errors, FSM, Tx).
  - Add aggregates with transition methods.
  - Split each context into domain/service/postgres/http.
  - Add context-local read queries in place of cross-context imports.
  - Add response structs, Postgres-backed tests and the import-rule test (`internal/arch`).
- [x] **F1.6 Clean Architecture + River:**
  - Layers as packages, a webhook ingest use case, ISP ports and routes per context.
  - Domain events published through River (D-006, D-007).
- [ ] **F2 Idempotency + wallet + void + client notifications** (detailed plan in `docs/next-session.md`):
  - Idempotency middleware (D-010), built first.
  - Payment profiles (saved cards).
  - Partial and total refunds (D-008), with the ledger following totvs-pay ADR-018.
  - Per-source webhook endpoints with signed, fan-out delivery (D-009).
  - Workspace mutation requests (update, disable, reactivate) as River jobs inserted in the request transaction.
- [ ] **F3 Pricing:** (add `kernel.Money` here, when fee arithmetic needs it)
  - Fee plans with rates (method × installment × brand, % + fixed, anticipation).
  - External codes per provider.
  - Workspace pricing setting (minimum amount enforced on one-shot).
  - Operator admin with basic auth.
- [ ] **F4 Insights:**
  - Bill analytics computed from our DB: index, show, summary, by method, volume by interval, declined reasons, authorization rate.
  - Conciliations and receivables through an optional `psp.Reporter`.
- [ ] **F5 Stripe:**
  - `psp/stripe` adapter via `stripe-go`.
  - Pick the provider per workspace through `provider_refs`.

Deliberately out of scope until a provider requires it: KYC documents, accreditation expiry, legal representatives, bank accounts, audit trail.
