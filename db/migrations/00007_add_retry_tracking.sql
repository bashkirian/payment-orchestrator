-- Add retry tracking columns to payouts table
ALTER TABLE payouts
    ADD COLUMN global_retry_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN provider_retry_count INTEGER NOT NULL DEFAULT 0;

-- Add 'retrying' state to the enum
ALTER TABLE payouts
    DROP CONSTRAINT payouts_state_check;

ALTER TABLE payouts
    ADD CONSTRAINT payouts_state_check
    CHECK (state IN ('created', 'queued', 'pending', 'processing', 'retrying', 'completed', 'sent', 'failed', 'canceled'));

-- Create index for retry worker to find pending retries
CREATE INDEX idx_payouts_state_retry ON payouts(state, global_retry_count)
    WHERE state = 'retrying';
