DROP INDEX IF EXISTS idx_device_org_building;
ALTER TABLE device
    DROP CONSTRAINT IF EXISTS fk_device_building,
    DROP COLUMN IF EXISTS building_id;
