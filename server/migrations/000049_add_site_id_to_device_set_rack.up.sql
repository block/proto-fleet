-- Add site_id to device_set_rack so racks can be directly attached to a
-- site without going through a building. Cascade-on-delete sets only
-- the site_id column to NULL; building_id is untouched here.
ALTER TABLE device_set_rack
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_device_set_rack_site FOREIGN KEY (site_id, org_id)
        REFERENCES site(id, org_id) ON DELETE SET NULL (site_id);

CREATE INDEX idx_device_set_rack_site ON device_set_rack(org_id, site_id);

-- Backfill site_id for any pre-existing racks already linked to a
-- building that itself has a site. Without this, ListSites.rack_count
-- and FindDeviceSiteConflicts (which both read device_set_rack.site_id
-- directly) would treat those racks as unassigned until each building
-- was manually reassigned. Today no rack writer populates building_id
-- in prod, so this is expected to update zero rows — but the statement
-- future-proofs the migration if any such row exists when it runs.
UPDATE device_set_rack dsr
SET site_id = b.site_id
FROM building b
WHERE dsr.building_id IS NOT NULL
  AND b.id = dsr.building_id
  AND b.org_id = dsr.org_id
  AND b.site_id IS NOT NULL;
