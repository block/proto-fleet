-- Multi-site support: nullable `site_id` on `device`. Existing devices stay
-- NULL ("Unassigned"); operators bulk-assign post-migration. ON DELETE
-- SET NULL matches the cascade-unassign semantics in J3 of the plan
-- (deleting a site moves its devices to the Unassigned bucket).

ALTER TABLE device
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_device_site FOREIGN KEY (site_id)
        REFERENCES site(id) ON DELETE SET NULL;

CREATE INDEX idx_device_org_site ON device(org_id, site_id);
