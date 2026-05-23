-- +goose Up
-- Allow NULL provider during payout creation (set after routing decision)
ALTER TABLE payouts ALTER COLUMN provider DROP NOT NULL;

-- +goose Down
-- Restore NOT NULL constraint (may fail if NULL providers exist)
UPDATE payouts SET provider = 'stripe' WHERE provider IS NULL;
ALTER TABLE payouts ALTER COLUMN provider SET NOT NULL;
