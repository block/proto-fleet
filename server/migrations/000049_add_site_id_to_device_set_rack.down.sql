DROP INDEX IF EXISTS idx_device_set_rack_site;

ALTER TABLE device_set_rack
    DROP CONSTRAINT IF EXISTS fk_device_set_rack_site;

ALTER TABLE device_set_rack
    DROP COLUMN IF EXISTS site_id;
