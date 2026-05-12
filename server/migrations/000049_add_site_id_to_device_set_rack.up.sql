-- Add site_id to device_set_rack so racks can be directly attached to a
-- site without going through a building. Cascade-on-delete sets only
-- the site_id column to NULL; building_id is untouched here.
ALTER TABLE device_set_rack
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_device_set_rack_site FOREIGN KEY (site_id, org_id)
        REFERENCES site(id, org_id) ON DELETE SET NULL (site_id);

CREATE INDEX idx_device_set_rack_site ON device_set_rack(org_id, site_id);
