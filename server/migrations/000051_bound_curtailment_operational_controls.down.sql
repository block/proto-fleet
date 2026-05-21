DROP INDEX IF EXISTS uq_curtailment_event_one_non_terminal_per_org;

ALTER TABLE curtailment_event
    DROP CONSTRAINT IF EXISTS ck_curtailment_event_restore_interval_bounds,
    DROP CONSTRAINT IF EXISTS ck_curtailment_event_max_duration_bounds;
