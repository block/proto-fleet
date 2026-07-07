-- Covering partial index for the active curtailment unavailable-reason
-- rollup. The hot active-events poll path already has an index-only state
-- rollup over (curtailment_event_id, state); this keeps the extra reason
-- aggregate scoped to unavailable targets and avoids heap reads for
-- last_error reason buckets between write bursts.
CREATE INDEX CONCURRENTLY idx_curtailment_target_unavailable_reason
    ON curtailment_target (curtailment_event_id, last_error)
    WHERE state = 'unavailable';
