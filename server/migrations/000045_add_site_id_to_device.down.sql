DROP INDEX IF EXISTS idx_device_org_site;
ALTER TABLE device
    DROP CONSTRAINT IF EXISTS fk_device_site,
    DROP COLUMN IF EXISTS site_id;
