DROP INDEX IF EXISTS idx_cohort_membership_site;
DROP INDEX IF EXISTS idx_cohort_membership_cohort;
DROP TABLE IF EXISTS cohort_membership;

DROP TRIGGER IF EXISTS update_cohort_updated_at ON cohort;
DROP INDEX IF EXISTS idx_cohort_org_state;
DROP INDEX IF EXISTS idx_cohort_expiry;
DROP INDEX IF EXISTS idx_cohort_owner_active;
DROP INDEX IF EXISTS uq_cohort_idempotency;
DROP INDEX IF EXISTS uq_cohort_one_default_per_org;
DROP TABLE IF EXISTS cohort;
