-- Multi-site support: nullable `building_id` on `device_set_rack` plus a
-- zone-string -> building backfill. Each unique non-null `zone` string per
-- org is promoted to a building row with `site_id IS NULL`; racks point at
-- their building. Racks with `zone IS NULL` (or empty) get
-- `building_id = NULL`. The `zone` column itself stays in place; it is
-- redundant after this migration and is dropped in a follow-up migration
-- once a writer audit confirms no callers remain.
-- See docs/plans/2026-05-05-multi-site-support-plan.md (J5).

ALTER TABLE device_set_rack
    ADD COLUMN building_id BIGINT NULL,
    ADD CONSTRAINT fk_device_set_rack_building FOREIGN KEY (building_id)
        REFERENCES building(id) ON DELETE SET NULL;

-- Promote each unique non-null zone per org into a building row. NOT EXISTS
-- guard makes the insert idempotent so `up && down && up` round-trips
-- without duplicating auto-promoted buildings.
INSERT INTO building (org_id, name)
SELECT DISTINCT ds.org_id, dsr.zone
FROM device_set_rack dsr
JOIN device_set ds ON dsr.device_set_id = ds.id
WHERE dsr.zone IS NOT NULL
  AND dsr.zone <> ''
  AND ds.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM building b
      WHERE b.org_id = ds.org_id
        AND b.name = dsr.zone
        AND b.site_id IS NULL
        AND b.deleted_at IS NULL
  );

-- Point each rack at the building matching its zone.
UPDATE device_set_rack dsr
SET building_id = b.id
FROM device_set ds, building b
WHERE dsr.device_set_id = ds.id
  AND dsr.zone IS NOT NULL
  AND dsr.zone <> ''
  AND b.org_id = ds.org_id
  AND b.name = dsr.zone
  AND b.site_id IS NULL
  AND b.deleted_at IS NULL;

CREATE INDEX idx_device_set_rack_building
    ON device_set_rack(building_id);
