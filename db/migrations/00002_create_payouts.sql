-- +goose Up
CREATE TABLE payouts (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    state        text        NOT NULL CHECK (state IN ('pending', 'processing', 'succeeded', 'failed')),
    amount_cents bigint      NOT NULL CHECK (amount_cents > 0),
    currency     text        NOT NULL,
    rail         text        NOT NULL CHECK (rail IN ('card', 'crypto')),
    provider     text        NOT NULL CHECK (provider IN ('stripe', 'crypto_sim', 'mock_card')),
    external_id  text        NULL UNIQUE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_payouts_state      ON payouts (state);
CREATE INDEX idx_payouts_created_at ON payouts (created_at);

CREATE TABLE idempotency_keys (
    key          text        PRIMARY KEY,
    request_hash text        NOT NULL,
    payout_id    uuid        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- Index to support TTL cleanup jobs
CREATE INDEX idx_idempotency_keys_created_at ON idempotency_keys (created_at);

-- +goose Down
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS payouts;
