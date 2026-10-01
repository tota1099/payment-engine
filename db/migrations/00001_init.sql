-- +goose Up
CREATE TABLE accounts (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    name            text NOT NULL,
    email           text NOT NULL,
    document_type   text NOT NULL CHECK (document_type IN ('cpf', 'cnpj')),
    document_number text NOT NULL,
    sources         text[] NOT NULL CHECK (cardinality(sources) > 0),
    status          text NOT NULL DEFAULT 'pending',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (document_type, document_number)
);

CREATE TABLE workspaces (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    account_id      uuid NOT NULL REFERENCES accounts (id),
    name            text NOT NULL,
    email           text NOT NULL,
    document_type   text NOT NULL CHECK (document_type IN ('cpf', 'cnpj')),
    document_number text NOT NULL,
    mcc             text NOT NULL CHECK (length(mcc) = 4),
    status          text NOT NULL DEFAULT 'pending',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON workspaces (account_id, id);

CREATE TABLE checkouts (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    workspace_id       uuid NOT NULL REFERENCES workspaces (id),
    name               text NOT NULL,
    items              jsonb NOT NULL CHECK (jsonb_typeof(items) = 'array' AND jsonb_array_length(items) > 0),
    payment_methods    text[] NOT NULL CHECK (cardinality(payment_methods) > 0),
    max_uses           int CHECK (max_uses > 0),
    expires_at         timestamptz,
    external_reference text,
    url                text NOT NULL,
    status             text NOT NULL DEFAULT 'active',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON checkouts (workspace_id, id);

CREATE TABLE bills (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    workspace_id    uuid NOT NULL REFERENCES workspaces (id),
    checkout_id     uuid REFERENCES checkouts (id),
    amount          bigint NOT NULL CHECK (amount > 0),
    currency        text NOT NULL DEFAULT 'BRL',
    payment_method  text NOT NULL,
    installments    int NOT NULL DEFAULT 1 CHECK (installments BETWEEN 1 AND 21),
    status          text NOT NULL DEFAULT 'pending',
    idempotency_key text NOT NULL,
    captured_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, idempotency_key)
);
CREATE INDEX ON bills (checkout_id, id);

CREATE TABLE transactions (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    bill_id             uuid NOT NULL REFERENCES bills (id),
    amount              bigint NOT NULL,
    status              text NOT NULL,
    code                text NOT NULL,
    provider            text NOT NULL,
    idempotency_key     text NOT NULL,
    provider_created_at timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (bill_id, idempotency_key)
);

-- Where an entity lives at a PSP. A new provider is a row, never a column.
CREATE TABLE provider_refs (
    entity_type text NOT NULL,
    entity_id   uuid NOT NULL,
    provider    text NOT NULL,
    external_id text NOT NULL,
    data        jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (entity_type, entity_id, provider),
    UNIQUE (provider, external_id)
);

CREATE TABLE webhook_events (
    id              bigserial PRIMARY KEY,
    provider        text NOT NULL,
    idempotency_key text NOT NULL,
    event_type      text NOT NULL,
    payload         jsonb NOT NULL,
    processed_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, idempotency_key)
);

CREATE TABLE outbox_events (
    id             bigserial PRIMARY KEY,
    event_type     text NOT NULL,
    payload        jsonb NOT NULL DEFAULT '{}',
    correlation_id text,
    attempts       int NOT NULL DEFAULT 0,
    fetched_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE outbox_events, webhook_events, provider_refs, transactions, bills, checkouts, workspaces, accounts;
