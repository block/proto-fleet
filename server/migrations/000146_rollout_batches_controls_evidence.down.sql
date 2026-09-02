ALTER TABLE firmware_rollout_device
    ADD COLUMN cohort TEXT NOT NULL DEFAULT 'rest';

UPDATE firmware_rollout_device SET cohort = 'pilot' WHERE batch_index = 0;

ALTER TABLE firmware_rollout_device
    DROP COLUMN batch_index,
    DROP COLUMN baseline_status,
    DROP COLUMN baseline_hash_rate_hs,
    DROP COLUMN baseline_open_errors,
    DROP COLUMN baseline_at;

UPDATE firmware_rollout SET stage = 'pilot' WHERE stage = 'batch';

ALTER TABLE firmware_rollout
    DROP COLUMN batch_count,
    DROP COLUMN current_batch,
    DROP COLUMN stage_changed_at,
    DROP COLUMN paused_at,
    DROP COLUMN auto_advance,
    DROP COLUMN max_hashrate_drop_percent,
    DROP COLUMN stabilization_seconds,
    DROP COLUMN previous_firmware_file_id,
    DROP COLUMN previous_firmware_version,
    DROP COLUMN cancel_reason;

ALTER TABLE firmware_rollout
    RENAME COLUMN batch_size TO pilot_count;
