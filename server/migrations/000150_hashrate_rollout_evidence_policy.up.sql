ALTER TABLE firmware_rollout
    ADD COLUMN hashrate_policy_max_drop_basis_points INT NULL,
    ADD COLUMN hashrate_policy_healthy_duration_seconds INT NULL,
    ADD CONSTRAINT ck_firmware_rollout_hashrate_policy_pair
        CHECK (
            (hashrate_policy_max_drop_basis_points IS NULL
                AND hashrate_policy_healthy_duration_seconds IS NULL)
            OR
            (hashrate_policy_max_drop_basis_points IS NOT NULL
                AND hashrate_policy_healthy_duration_seconds IS NOT NULL)
        ),
    ADD CONSTRAINT ck_firmware_rollout_hashrate_policy_max_drop
        CHECK (
            hashrate_policy_max_drop_basis_points IS NULL
            OR (
                hashrate_policy_max_drop_basis_points BETWEEN 0 AND 10000
                AND hashrate_policy_max_drop_basis_points % 10 = 0
            )
        ),
    ADD CONSTRAINT ck_firmware_rollout_hashrate_policy_healthy_duration
        CHECK (
            hashrate_policy_healthy_duration_seconds IS NULL
            OR (
                hashrate_policy_healthy_duration_seconds BETWEEN 10 AND 1800
                AND hashrate_policy_healthy_duration_seconds % 10 = 0
            )
        );

ALTER TABLE firmware_rollout_batch
    ADD COLUMN completed_at TIMESTAMPTZ NULL,
    ADD COLUMN evidence_status TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN evidence_total_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN evidence_paired_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN cumulative_baseline_hashrate_hs DOUBLE PRECISION NULL,
    ADD COLUMN cumulative_current_hashrate_hs DOUBLE PRECISION NULL,
    ADD COLUMN cumulative_delta_basis_points INT NULL,
    ADD COLUMN latest_policy_bucket_hashrate_hs DOUBLE PRECISION NULL,
    ADD COLUMN latest_policy_bucket_delta_basis_points INT NULL,
    ADD COLUMN healthy_since TIMESTAMPTZ NULL,
    ADD COLUMN last_policy_bucket_boundary TIMESTAMPTZ NULL,
    ADD COLUMN evaluated_at TIMESTAMPTZ NULL,
    ADD COLUMN evidence_error_message TEXT NULL,
    ADD COLUMN post_window_finalized BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN post_window_finalized_at TIMESTAMPTZ NULL,
    ADD CONSTRAINT ck_firmware_rollout_batch_evidence_status
        CHECK (evidence_status IN (
            'pending',
            'collecting',
            'unavailable',
            'observing',
            'healthy',
            'held',
            'stale',
            'automation_error',
            'finalized'
        )),
    ADD CONSTRAINT ck_firmware_rollout_batch_evidence_counts
        CHECK (
            evidence_total_count >= 0
            AND evidence_paired_count >= 0
            AND evidence_paired_count <= evidence_total_count
        ),
    ADD CONSTRAINT ck_firmware_rollout_batch_post_window_finalization
        CHECK (
            (NOT post_window_finalized AND post_window_finalized_at IS NULL)
            OR
            (post_window_finalized AND post_window_finalized_at IS NOT NULL)
        );

CREATE INDEX idx_firmware_rollout_batch_evidence_candidates
    ON firmware_rollout_batch(completed_at, id)
    WHERE state = 'completed' AND NOT post_window_finalized;
