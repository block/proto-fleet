DROP TRIGGER IF EXISTS update_cohort_firmware_target_updated_at ON cohort_firmware_target;
DROP INDEX IF EXISTS idx_cohort_firmware_target_org_type;
DROP INDEX IF EXISTS uq_cohort_firmware_target_canonical_type;
DROP TABLE IF EXISTS cohort_firmware_target;
