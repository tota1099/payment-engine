# Domain language

Use these names in code, API and docs. Status values are typed constants in the owning context.

## Account management (`internal/account`)

**Account**: the tenant. It is owned by one or more **sources**, the API consumers identified by their API key (`Authorization: Bearer`).
Statuses: `pending → active → canceled`. There is no KYC step yet.

**Workspace**: a seller under an account, registered at the PSP as a **connected account**.
Statuses: `pending → accrediting → active | rejected`, `active → suspended | canceled`, `canceled → active`.
Activation and rejection arrive only by webhook.

## Payment orchestration (`internal/checkout`, `internal/billing`)

**Checkout**: a hosted payment link. It defines items, accepted **payment methods** (`credit_card`, `pix`, `boleto`), and optionally `max_uses` and `expires_at`.
Statuses: `active ⇄ disabled`, either `→ canceled`. Disabling and reactivating require a checkout that has not expired.

**Bill**: one charge attempt, optionally tied to a checkout, keyed by the client's **Idempotency-Key**.
Statuses: `pending → authorized → captured → charged_back`, `pending|authorized → voided`, any status `→ error`.
A declined charge ends in `error`.

**Captured**: the paid moment. Avoid "paid", "settled" and "completed" in domain code. `captured_at` is the PSP's timestamp.

**Transaction**: one fact the PSP reported about a bill: the synchronous charge response, or a webhook event. It is append-only and deduplicated by `(bill_id, idempotency_key)`.

**One-shot payment**: charging a bill directly with a card `token_id` (tokenized on the client side) or, from F2, a saved payment profile. A raw PAN never enters the engine.

## Integration

**Provider ref**: the row mapping one of our entities to its id at a PSP (`provider_refs`). A new provider is a new row, never a new column.

**Webhook event**: an inbound PSP event, deduplicated by `(provider, event id)`.

**Domain event**: a fact an aggregate records when it changes (`BillUpdateCompleted`, `WorkspaceActivationCompleted`). It is published to consumers as `bill_update_completed` / `workspace_activation_completed`, and enqueued as a River job in the same transaction as the change it reports.
