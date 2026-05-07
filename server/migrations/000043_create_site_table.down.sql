DROP TRIGGER IF EXISTS update_site_updated_at ON site;
DROP INDEX IF EXISTS idx_site_org_deleted;
DROP INDEX IF EXISTS uk_site_org_name;
DROP TABLE IF EXISTS site;
