-- The curtailment_event normalization in the up migration is intentionally not
-- reversed: after rows are rewritten from the old "0 means server default"
-- shape to the stamped effective size, there is no reliable provenance bit to
-- distinguish them from explicit positive restore sizes.

UPDATE curtailment_response_profile
SET restore_batch_size = 50
WHERE restore_batch_size = 0;

ALTER TABLE curtailment_response_profile
    ALTER COLUMN restore_batch_size SET DEFAULT 50,
    ALTER COLUMN restore_batch_interval_sec SET DEFAULT 5;

ALTER TABLE curtailment_response_profile
    DROP CONSTRAINT ck_curtailment_response_profile_restore_batch_size,
    ADD CONSTRAINT ck_curtailment_response_profile_restore_batch_size
        CHECK (restore_batch_size > 0 AND restore_batch_size <= 10000);
