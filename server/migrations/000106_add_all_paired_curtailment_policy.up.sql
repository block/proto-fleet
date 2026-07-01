ALTER TABLE curtailment_event
    ADD COLUMN force_include_all_paired_miners BOOLEAN NOT NULL DEFAULT FALSE,
    ADD CONSTRAINT ck_curtailment_event_all_paired_full_fleet
        CHECK (NOT force_include_all_paired_miners OR mode = 'FULL_FLEET');

ALTER TABLE curtailment_response_profile
    ADD COLUMN force_include_all_paired_miners BOOLEAN NOT NULL DEFAULT FALSE,
    ADD CONSTRAINT ck_curtailment_response_profile_all_paired_full_fleet
        CHECK (NOT force_include_all_paired_miners OR mode = 'FULL_FLEET');

ALTER TABLE curtailment_target
    DROP CONSTRAINT ck_curtailment_target_curtail_state,
    DROP CONSTRAINT ck_curtailment_target_restore_state;

ALTER TABLE curtailment_target
    ADD CONSTRAINT ck_curtailment_target_curtail_state
        CHECK (curtail_state IN ('pending', 'dispatching', 'dispatched', 'confirmed', 'drifted', 'unavailable', 'resolved', 'released', 'restore_failed')) NOT VALID,
    ADD CONSTRAINT ck_curtailment_target_restore_state
        CHECK (restore_state IS NULL OR restore_state IN ('pending', 'dispatching', 'dispatched', 'confirmed', 'drifted', 'unavailable', 'resolved', 'released', 'restore_failed')) NOT VALID;

ALTER TABLE curtailment_target
    VALIDATE CONSTRAINT ck_curtailment_target_curtail_state,
    VALIDATE CONSTRAINT ck_curtailment_target_restore_state;
