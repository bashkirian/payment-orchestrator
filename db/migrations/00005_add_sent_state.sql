-- +goose Up
ALTER TABLE payouts DROP CONSTRAINT payouts_state_check;
ALTER TABLE payouts ADD CONSTRAINT payouts_state_check
    CHECK (state IN ('created', 'queued', 'pending', 'processing', 'completed', 'succeeded', 'sent', 'failed', 'canceled'));

-- +goose Down
ALTER TABLE payouts DROP CONSTRAINT payouts_state_check;
ALTER TABLE payouts ADD CONSTRAINT payouts_state_check
    CHECK (state IN ('created', 'queued', 'pending', 'processing', 'completed', 'succeeded', 'failed', 'canceled'));
