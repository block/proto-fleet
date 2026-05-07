-- Multi-site support: nullable `site_id` on `device`. Existing devices stay
-- NULL ("Unassigned"); operators bulk-assign post-migration. ON DELETE
-- SET NULL matches the cascade-unassign semantics in J3 of the plan
-- (deleting a site moves its devices to the Unassigned bucket).
--
-- Composite FK on (site_id, org_id) -> site(id, org_id) blocks cross-tenant
-- assignments at the DB level. Bulk-reassign and other future writers can
-- update site_id without re-validating org membership in service code.

-- ON DELETE SET NULL (site_id): Postgres 15+ column list — when the
-- referenced site is deleted, only `device.site_id` is nulled, not
-- `device.org_id` (which is NOT NULL). Without the column list, a
-- composite-FK SET NULL would attempt to null both columns and fail.
ALTER TABLE device
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_device_site FOREIGN KEY (site_id, org_id)
        REFERENCES site(id, org_id) ON DELETE SET NULL (site_id);

CREATE INDEX idx_device_org_site ON device(org_id, site_id);
