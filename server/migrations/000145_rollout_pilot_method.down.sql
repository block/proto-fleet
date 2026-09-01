ALTER TABLE firmware_rollout_device
    DROP COLUMN cohort;

-- Restore the pre-pilot invariant: a row exists only once a command was sent.
DELETE FROM firmware_rollout_device WHERE update_sent_at IS NULL;

ALTER TABLE firmware_rollout_device
    ALTER COLUMN update_sent_at SET DEFAULT now(),
    ALTER COLUMN update_sent_at SET NOT NULL;

ALTER TABLE firmware_rollout
    DROP COLUMN method,
    DROP COLUMN stage,
    DROP COLUMN pilot_count;
