CREATE INDEX CONCURRENTLY idx_firmware_rollout_batch_evidence_candidates
    ON firmware_rollout_batch(
        evaluated_at ASC NULLS FIRST,
        completed_at,
        id
    )
    WHERE state = 'completed'
      AND completed_at IS NOT NULL
      AND NOT post_window_finalized;
