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
