ALTER TABLE curtailment_event
    DROP CONSTRAINT IF EXISTS ck_curtailment_event_restore_interval_bounds,
    DROP CONSTRAINT IF EXISTS ck_curtailment_event_max_duration_bounds;
