DROP INDEX IF EXISTS idx_curtailment_response_profile_facility_fans;

ALTER TABLE curtailment_response_profile
    DROP CONSTRAINT IF EXISTS ck_curtailment_response_profile_fan_off_delay,
    DROP CONSTRAINT IF EXISTS ck_curtailment_response_profile_fan_restore_delay,
    DROP COLUMN IF EXISTS facility_fan_device_ids,
    DROP COLUMN IF EXISTS fan_off_delay_sec,
    DROP COLUMN IF EXISTS fan_restore_delay_sec;
