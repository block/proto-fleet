DROP TRIGGER IF EXISTS update_building_updated_at ON building;
DROP INDEX IF EXISTS idx_building_site_deleted;
DROP INDEX IF EXISTS idx_building_org_deleted;
DROP INDEX IF EXISTS uk_building_site_name;
DROP TABLE IF EXISTS building;
