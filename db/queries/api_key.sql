-- name: APIKeyCreate :exec
INSERT INTO api_keys (key_hash, source) VALUES ($1, $2);

-- name: APIKeySource :one
SELECT source FROM api_keys WHERE key_hash = $1;
