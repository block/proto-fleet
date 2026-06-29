-- Existing curtailment_event rows created before immediate restore used
-- restore_batch_size=0 as "server default" while effective_batch_size carried
-- the actual stamped restore throttle. Normalize those unshipped legacy rows
-- before 0 becomes the persisted immediate-restore sentinel.
UPDATE curtailment_event
SET restore_batch_size = effective_batch_size
WHERE restore_batch_size = 0
    AND effective_batch_size IS NOT NULL;

ALTER TABLE curtailment_response_profile
    ALTER COLUMN restore_batch_size SET DEFAULT 0,
    ALTER COLUMN restore_batch_interval_sec SET DEFAULT 0;

ALTER TABLE curtailment_response_profile
    DROP CONSTRAINT ck_curtailment_response_profile_restore_batch_size,
    ADD CONSTRAINT ck_curtailment_response_profile_restore_batch_size
        CHECK (restore_batch_size >= 0 AND restore_batch_size <= 10000);
