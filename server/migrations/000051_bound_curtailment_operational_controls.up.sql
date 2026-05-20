ALTER TABLE curtailment_event
    ADD CONSTRAINT ck_curtailment_event_max_duration_bounds
        CHECK (max_duration_seconds IS NULL OR (max_duration_seconds > 0 AND max_duration_seconds <= 604800)),
    ADD CONSTRAINT ck_curtailment_event_restore_interval_bounds
        CHECK (restore_batch_interval_sec >= 0 AND restore_batch_interval_sec <= 3600);
