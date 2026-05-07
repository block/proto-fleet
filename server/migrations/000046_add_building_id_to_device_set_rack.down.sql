-- Drop only the columns this migration added. Auto-promoted building
-- rows are intentionally preserved: an operator may have edited them
-- post-upgrade, and the up migration is idempotent so re-applying up
-- does not double-insert.
DROP INDEX IF EXISTS idx_device_set_rack_building;
ALTER TABLE device_set_rack
    DROP CONSTRAINT IF EXISTS fk_device_set_rack_building,
    DROP COLUMN IF EXISTS building_id,
    DROP COLUMN IF EXISTS org_id;
