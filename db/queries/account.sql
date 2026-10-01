-- name: AccountCreate :one
INSERT INTO accounts (name, email, document_type, document_number, sources)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: AccountGet :one
SELECT * FROM accounts WHERE id = $1;

-- name: AccountGetForUpdate :one
SELECT * FROM accounts WHERE id = $1 FOR UPDATE;

-- name: AccountUpdateStatus :one
UPDATE accounts SET status = $2, updated_at = now() WHERE id = $1 RETURNING updated_at;

-- name: AccountWorkspaceCreate :one
INSERT INTO workspaces (account_id, name, email, document_type, document_number, mcc)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: AccountWorkspaceGet :one
SELECT * FROM workspaces WHERE id = $1 AND account_id = $2;

-- name: AccountWorkspaceGetForUpdate :one
SELECT * FROM workspaces WHERE id = $1 FOR UPDATE;

-- name: AccountWorkspaceList :many
SELECT * FROM workspaces
WHERE account_id = $1 AND (sqlc.narg(starting_after)::uuid IS NULL OR id < sqlc.narg(starting_after))
ORDER BY id DESC
LIMIT $2;

-- name: AccountWorkspaceUpdateStatus :one
UPDATE workspaces SET status = $2, updated_at = now() WHERE id = $1 RETURNING updated_at;
