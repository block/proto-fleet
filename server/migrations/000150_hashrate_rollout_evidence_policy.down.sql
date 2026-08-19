DROP INDEX IF EXISTS idx_firmware_rollout_batch_evidence_candidates;

ALTER TABLE firmware_rollout_batch
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_batch_post_window_finalization,
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_batch_evidence_counts,
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_batch_evidence_status,
    DROP COLUMN IF EXISTS post_window_finalized_at,
    DROP COLUMN IF EXISTS post_window_finalized,
    DROP COLUMN IF EXISTS evidence_error_message,
    DROP COLUMN IF EXISTS evaluated_at,
    DROP COLUMN IF EXISTS last_policy_bucket_boundary,
    DROP COLUMN IF EXISTS healthy_since,
    DROP COLUMN IF EXISTS latest_policy_bucket_delta_basis_points,
    DROP COLUMN IF EXISTS latest_policy_bucket_hashrate_hs,
    DROP COLUMN IF EXISTS cumulative_delta_basis_points,
    DROP COLUMN IF EXISTS cumulative_current_hashrate_hs,
    DROP COLUMN IF EXISTS cumulative_baseline_hashrate_hs,
    DROP COLUMN IF EXISTS evidence_paired_count,
    DROP COLUMN IF EXISTS evidence_total_count,
    DROP COLUMN IF EXISTS evidence_status,
    DROP COLUMN IF EXISTS completed_at;

ALTER TABLE firmware_rollout
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_hashrate_policy_healthy_duration,
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_hashrate_policy_max_drop,
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_hashrate_policy_pair,
    DROP COLUMN IF EXISTS hashrate_policy_healthy_duration_seconds,
    DROP COLUMN IF EXISTS hashrate_policy_max_drop_basis_points;
