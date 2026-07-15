DROP INDEX IF EXISTS idx_cohort_membership_site;

ALTER TABLE cohort_membership
    DROP CONSTRAINT IF EXISTS fk_cohort_membership_site;

ALTER TABLE cohort_membership
    DROP COLUMN IF EXISTS site_id;
