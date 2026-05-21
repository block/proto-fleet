ALTER TABLE curtailment_event
    ADD CONSTRAINT ck_curtailment_event_max_duration_bounds
        CHECK (max_duration_seconds IS NULL OR (max_duration_seconds > 0 AND max_duration_seconds <= 604800)),
    ADD CONSTRAINT ck_curtailment_event_restore_interval_bounds
        CHECK (restore_batch_interval_sec >= 0 AND restore_batch_interval_sec <= 3600);

-- One non-terminal curtailment event per org. Closes the cross-event
-- overlap window between selector and insert: a concurrent Start that
-- raced the selector check will fail at insert time with unique_violation,
-- mapped to AlreadyExists at the service boundary.
CREATE UNIQUE INDEX uq_curtailment_event_one_non_terminal_per_org
    ON curtailment_event (org_id)
    WHERE state IN ('pending', 'active', 'restoring');
