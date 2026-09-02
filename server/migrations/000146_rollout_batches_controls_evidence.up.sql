-- Generalizes pilot rollouts into batches, adds operator controls (pause,
-- abort with assignment restore) and per-miner baseline evidence for the
-- review gate.

-- Rollout: batches replace the pilot-specific shape. A pilot is one batch
-- followed by the rest; fixed batches are N batches followed by the rest
-- (which covers late joiners).
ALTER TABLE firmware_rollout
    RENAME COLUMN pilot_count TO batch_size;

ALTER TABLE firmware_rollout
    ADD COLUMN batch_count INT NOT NULL DEFAULT 0,      -- snapshotted batches (0 for immediate)
    ADD COLUMN current_batch INT NOT NULL DEFAULT 0,    -- 0-based index of the batch in flight / just finished
    ADD COLUMN stage_changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN paused_at TIMESTAMPTZ NULL,
    ADD COLUMN auto_advance BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN max_hashrate_drop_percent DOUBLE PRECISION NULL,
    ADD COLUMN stabilization_seconds INT NOT NULL DEFAULT 0,
    ADD COLUMN previous_firmware_file_id TEXT NOT NULL DEFAULT '',  -- assignment in place before this rollout
    ADD COLUMN previous_firmware_version TEXT NOT NULL DEFAULT '',
    ADD COLUMN cancel_reason TEXT NOT NULL DEFAULT '';               -- superseded | aborted | cleared

-- The pilot stage is now the first batch.
UPDATE firmware_rollout SET stage = 'batch' WHERE stage = 'pilot';
UPDATE firmware_rollout SET batch_count = 1 WHERE method = 'pilot';
UPDATE firmware_rollout SET cancel_reason = 'superseded' WHERE status = 'canceled';

-- Rollout devices: the pilot cohort becomes batch 0; NULL means the device
-- is not part of any snapshotted batch (rest / late joiner). Baseline
-- health is captured when the device is snapshotted into the rollout.
ALTER TABLE firmware_rollout_device
    ADD COLUMN batch_index INT NULL,
    ADD COLUMN baseline_status TEXT NULL,
    ADD COLUMN baseline_hash_rate_hs DOUBLE PRECISION NULL,
    ADD COLUMN baseline_open_errors INT NULL,
    ADD COLUMN baseline_at TIMESTAMPTZ NULL;

UPDATE firmware_rollout_device SET batch_index = 0 WHERE cohort = 'pilot';

ALTER TABLE firmware_rollout_device
    DROP COLUMN cohort;
