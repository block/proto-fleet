ALTER TABLE curtailment_response_profile
    ALTER COLUMN restore_batch_size SET DEFAULT 0,
    ALTER COLUMN restore_batch_interval_sec SET DEFAULT 0;

ALTER TABLE curtailment_response_profile
    DROP CONSTRAINT ck_curtailment_response_profile_restore_batch_size,
    ADD CONSTRAINT ck_curtailment_response_profile_restore_batch_size
        CHECK (restore_batch_size >= 0 AND restore_batch_size <= 10000);
