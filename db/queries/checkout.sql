-- name: CheckoutWorkspace :one
SELECT w.id, w.status, coalesce(r.external_id, '')::text AS connected_account
FROM workspaces w
LEFT JOIN provider_refs r ON r.entity_type = 'workspace' AND r.entity_id = w.id AND r.provider = sqlc.arg(provider)
WHERE w.id = sqlc.arg(id) AND w.account_id = sqlc.arg(account_id);

-- name: CheckoutCreate :one
INSERT INTO checkouts (id, workspace_id, name, items, payment_methods, max_uses, expires_at, external_reference, url)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: CheckoutGet :one
SELECT * FROM checkouts WHERE id = $1 AND workspace_id = $2;

-- name: CheckoutGetForUpdate :one
SELECT * FROM checkouts WHERE id = $1 AND workspace_id = $2 FOR UPDATE;

-- name: CheckoutList :many
SELECT * FROM checkouts
WHERE workspace_id = $1 AND (sqlc.narg(starting_after)::uuid IS NULL OR id < sqlc.narg(starting_after))
ORDER BY id DESC
LIMIT $2;

-- name: CheckoutUpdateStatus :one
UPDATE checkouts SET status = $2, updated_at = now() WHERE id = $1 RETURNING updated_at;

-- name: CheckoutCountUses :one
SELECT count(*) FROM bills WHERE checkout_id = $1 AND status <> 'error';
