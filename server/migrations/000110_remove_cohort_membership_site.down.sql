ALTER TABLE cohort_membership
    ADD COLUMN IF NOT EXISTS site_id BIGINT NULL;

ALTER TABLE cohort_membership
    ADD CONSTRAINT fk_cohort_membership_site
    FOREIGN KEY (site_id, org_id)
    REFERENCES site(id, org_id)
    ON DELETE SET NULL (site_id);

CREATE INDEX IF NOT EXISTS idx_cohort_membership_site
    ON cohort_membership (org_id, site_id)
    WHERE site_id IS NOT NULL;
