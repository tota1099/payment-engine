-- +goose Up
-- Callers authenticate with "Authorization: Bearer <key>". The key resolves to
-- its source (consumer); only its SHA-256 is stored.
CREATE TABLE api_keys (
    key_hash   bytea PRIMARY KEY,
    source     text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE api_keys;
