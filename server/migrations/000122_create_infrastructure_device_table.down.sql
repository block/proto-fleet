DROP TRIGGER IF EXISTS update_infrastructure_device_updated_at ON infrastructure_device;
DROP INDEX IF EXISTS idx_infrastructure_device_site_deleted;
DROP INDEX IF EXISTS idx_infrastructure_device_org_deleted;
DROP INDEX IF EXISTS uk_infrastructure_device_site_name;
DROP TABLE IF EXISTS infrastructure_device;
