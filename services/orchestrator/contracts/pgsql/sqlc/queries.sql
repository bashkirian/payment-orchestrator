-- name: CreatePayout :one
INSERT INTO payouts (state, amount_cents, currency, rail, provider, external_id)
VALUES (@state, @amount_cents, @currency, @rail, @provider, @external_id)
RETURNING *;

-- name: GetPayout :one
SELECT * FROM payouts WHERE id = @id;

-- name: UpdatePayoutState :one
UPDATE payouts
SET state = @state, external_id = @external_id, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: TryInsertIdempotencyKey :one
INSERT INTO idempotency_keys (key, request_hash, payout_id)
VALUES (@key, @request_hash, @payout_id)
ON CONFLICT (key) DO NOTHING
RETURNING *;

-- name: GetIdempotencyKey :one
SELECT * FROM idempotency_keys WHERE key = @key;
