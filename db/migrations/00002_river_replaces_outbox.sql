-- +goose Up
-- Domain events now go to River jobs, inserted in the business transaction.
DROP TABLE outbox_events;

-- +goose Down
CREATE TABLE outbox_events (
    id             bigserial PRIMARY KEY,
    event_type     text NOT NULL,
    payload        jsonb NOT NULL DEFAULT '{}',
    correlation_id text,
    attempts       int NOT NULL DEFAULT 0,
    fetched_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);
