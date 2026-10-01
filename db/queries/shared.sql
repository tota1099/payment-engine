-- name: ProviderRefUpsert :exec
INSERT INTO provider_refs (entity_type, entity_id, provider, external_id, data)
VALUES ($1, $2, $3, $4, '{}')
ON CONFLICT (entity_type, entity_id, provider) DO UPDATE SET external_id = excluded.external_id;

-- name: ProviderRefExists :one
SELECT EXISTS (SELECT 1 FROM provider_refs WHERE entity_type = $1 AND entity_id = $2 AND provider = $3);

-- name: ProviderRefFind :one
SELECT * FROM provider_refs WHERE provider = $1 AND external_id = $2;

-- name: WebhookInsert :one
INSERT INTO webhook_events (provider, idempotency_key, event_type, payload)
VALUES ($1, $2, $3, $4)
ON CONFLICT (provider, idempotency_key) DO NOTHING
RETURNING id;

-- name: WebhookMarkProcessed :exec
UPDATE webhook_events SET processed_at = now() WHERE id = $1;
