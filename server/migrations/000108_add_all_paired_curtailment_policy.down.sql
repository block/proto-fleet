ALTER TABLE curtailment_target
    DROP CONSTRAINT ck_curtailment_target_curtail_state,
    DROP CONSTRAINT ck_curtailment_target_restore_state;

UPDATE curtailment_target
SET curtail_state = 'released'
WHERE curtail_state = 'unavailable';

UPDATE curtailment_target
SET state       = 'released',
    released_at = COALESCE(released_at, CURRENT_TIMESTAMP),
    last_error  = COALESCE(last_error, 'released during all-paired policy rollback')
WHERE state = 'unavailable';

UPDATE curtailment_target
SET restore_state = 'released'
WHERE restore_state = 'unavailable';

-- Re-add the pre-000108 constraints NOT VALID only. Existing rows were just
-- remapped above inside this same transaction, and new rows are checked on
-- write; skipping VALIDATE avoids holding the ACCESS EXCLUSIVE lock across a
-- full-table scan during a rollback (which typically runs mid-incident).
ALTER TABLE curtailment_target
    ADD CONSTRAINT ck_curtailment_target_curtail_state
        CHECK (curtail_state IN ('pending', 'dispatching', 'dispatched', 'confirmed', 'drifted', 'resolved', 'released', 'restore_failed')) NOT VALID,
    ADD CONSTRAINT ck_curtailment_target_restore_state
        CHECK (restore_state IS NULL OR restore_state IN ('pending', 'dispatching', 'dispatched', 'confirmed', 'drifted', 'resolved', 'released', 'restore_failed')) NOT VALID;

ALTER TABLE curtailment_response_profile
    DROP CONSTRAINT ck_curtailment_response_profile_all_paired_full_fleet,
    DROP COLUMN force_include_all_paired_miners;

ALTER TABLE curtailment_event
    DROP CONSTRAINT ck_curtailment_event_all_paired_full_fleet,
    DROP COLUMN force_include_all_paired_miners;
