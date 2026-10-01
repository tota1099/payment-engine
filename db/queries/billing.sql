-- name: BillingWorkspace :one
SELECT w.id, w.status, coalesce(r.external_id, '')::text AS connected_account
FROM workspaces w
LEFT JOIN provider_refs r ON r.entity_type = 'workspace' AND r.entity_id = w.id AND r.provider = sqlc.arg(provider)
WHERE w.id = sqlc.arg(id) AND w.account_id = sqlc.arg(account_id);

-- name: BillingCheckoutExists :one
SELECT EXISTS (SELECT 1 FROM checkouts WHERE id = $1 AND workspace_id = $2);

-- name: BillingInsert :one
INSERT INTO bills (workspace_id, checkout_id, amount, currency, payment_method, installments, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, idempotency_key) DO NOTHING
RETURNING *;

-- name: BillingGetByKey :one
SELECT * FROM bills WHERE workspace_id = $1 AND idempotency_key = $2;

-- name: BillingGet :one
SELECT * FROM bills WHERE id = $1 AND workspace_id = $2;

-- name: BillingGetForUpdate :one
SELECT * FROM bills WHERE id = $1 FOR UPDATE;

-- name: BillingUpdate :one
UPDATE bills SET status = $2, captured_at = $3, updated_at = now() WHERE id = $1 RETURNING updated_at;

-- name: BillingListByCheckout :many
SELECT * FROM bills
WHERE workspace_id = $1 AND checkout_id = $2
  AND (sqlc.narg(starting_after)::uuid IS NULL OR id < sqlc.narg(starting_after))
ORDER BY id DESC
LIMIT $3;

-- name: BillingInsertTransaction :exec
INSERT INTO transactions (bill_id, amount, status, code, provider, idempotency_key, provider_created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (bill_id, idempotency_key) DO NOTHING;

-- name: BillingTransactions :many
SELECT * FROM transactions WHERE bill_id = $1 ORDER BY id;
