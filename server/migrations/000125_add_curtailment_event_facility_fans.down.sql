DROP INDEX IF EXISTS idx_curtailment_event_active_facility_fans;

ALTER TABLE curtailment_event
    DROP CONSTRAINT IF EXISTS ck_curtailment_event_fan_off_delay,
    DROP CONSTRAINT IF EXISTS ck_curtailment_event_fan_restore_delay,
    DROP CONSTRAINT IF EXISTS ck_curtailment_event_fan_site_alignment,
    DROP COLUMN IF EXISTS facility_fan_device_ids,
    DROP COLUMN IF EXISTS facility_fan_site_ids,
    DROP COLUMN IF EXISTS fan_off_delay_sec,
    DROP COLUMN IF EXISTS fan_restore_delay_sec,
    DROP COLUMN IF EXISTS fan_off_sent_at,
    DROP COLUMN IF EXISTS fan_on_sent_at,
    DROP COLUMN IF EXISTS fan_last_error;
