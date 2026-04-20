-- Remove indexes that add write overhead without serving any live queries.

-- sort-by-name uses a TRIM(COALESCE(...manufacturer || ' ' || model)) expression,
-- which a plain (org_id, manufacturer, model, id) index cannot satisfy.
DROP INDEX IF EXISTS idx_discovered_device_sort_name;

-- Redundant: idx_discovered_device_org_active (org_id, is_active, deleted_at)
-- is a strict superset of this single-column index.
DROP INDEX IF EXISTS idx_discovered_device_org;

-- Redundant: the unique constraint on (org_id, device_identifier) covers all
-- lookups that also filter by org_id.
DROP INDEX IF EXISTS idx_discovered_device_identifier;

-- driver_name is only SELECTed, never used in WHERE or ORDER BY clauses.
DROP INDEX IF EXISTS idx_discovered_device_driver_name;
