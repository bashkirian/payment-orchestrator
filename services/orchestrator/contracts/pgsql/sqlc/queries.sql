-- name: CreatePayout :one
INSERT INTO payouts (state, amount_cents, currency, rail, provider, external_id, global_retry_count, provider_retry_count)
VALUES (@state, @amount_cents, @currency, @rail, sqlc.narg('provider')::text, @external_id, 0, 0)
RETURNING *;

-- name: GetPayout :one
SELECT * FROM payouts WHERE id = @id;

-- name: UpdatePayoutState :one
UPDATE payouts
SET state = @state, external_id = @external_id, provider = sqlc.narg('provider')::text, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: UpdatePayoutRetryState :one
-- Updates payout state and increments retry counters for retry processing.
UPDATE payouts
SET state = @state,
    global_retry_count = @global_retry_count,
    provider_retry_count = @provider_retry_count,
    provider = sqlc.narg('provider')::text,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: TryInsertIdempotencyKey :one
INSERT INTO idempotency_keys (key, request_hash, payout_id)
VALUES (@key, @request_hash, @payout_id)
ON CONFLICT (key) DO NOTHING
RETURNING *;

-- name: CancelPayoutIfCancelable :one
-- Atomically transitions a payout to canceled only if it is in a cancelable state.
-- Returns the updated row; pgx.ErrNoRows if not found or state not cancelable.
UPDATE payouts
SET state = 'canceled', updated_at = now()
WHERE id = @id
  AND state = ANY(@cancelable_states::text[])
RETURNING *;

-- name: GetIdempotencyKey :one
SELECT * FROM idempotency_keys WHERE key = @key;

-- name: FindPayoutByExternalID :one
SELECT * FROM payouts WHERE external_id = @external_id;
