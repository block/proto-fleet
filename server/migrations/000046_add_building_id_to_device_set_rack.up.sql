-- Multi-site support: nullable `building_id` on `device_set_rack` plus a
-- zone-string -> building backfill. Each unique non-null `zone` string per
-- org is promoted to a building row with `site_id IS NULL`; racks point at
-- their building. Racks with `zone IS NULL` (or empty) get
-- `building_id = NULL`. The `zone` column itself stays in place; it is
-- redundant after this migration and is dropped in a follow-up migration
-- once a writer audit confirms no callers remain.
-- See docs/plans/2026-05-05-multi-site-support-plan.md (J5).

-- Denormalize `org_id` onto `device_set_rack` so the building FK can be
-- composite-keyed against `building(id, org_id)` and Postgres rejects
-- cross-tenant rack/building pointers at the DB layer. Pattern matches
-- `device_set_membership.org_id`. Backfill from `device_set` first, then
-- promote to NOT NULL.
ALTER TABLE device_set_rack ADD COLUMN org_id BIGINT NULL;

UPDATE device_set_rack dsr
SET org_id = ds.org_id
FROM device_set ds
WHERE dsr.device_set_id = ds.id;

ALTER TABLE device_set_rack
    ALTER COLUMN org_id SET NOT NULL;

-- Composite-key target on device_set so device_set_rack can FK on
-- (device_set_id, org_id) and Postgres rejects any rack whose
-- denormalized org_id drifts from its parent device_set.org_id.
-- Without this FK, a future writer that updates both
-- device_set_rack.org_id and device_set_rack.building_id together
-- could attach an org B rack to an org A device_set while satisfying
-- the building FK. Same pattern as `site.uq_site_id_org_id` and
-- `building.uq_building_id_org_id`.
ALTER TABLE device_set
    ADD CONSTRAINT uq_device_set_id_org_id UNIQUE (id, org_id);

ALTER TABLE device_set_rack
    ADD CONSTRAINT fk_device_set_rack_device_set_org FOREIGN KEY (device_set_id, org_id)
        REFERENCES device_set(id, org_id);

ALTER TABLE device_set_rack
    ADD COLUMN building_id BIGINT NULL,
    -- Composite FK with column-list SET NULL so building deletion only
    -- nulls building_id, not the NOT NULL org_id.
    ADD CONSTRAINT fk_device_set_rack_building FOREIGN KEY (building_id, org_id)
        REFERENCES building(id, org_id) ON DELETE SET NULL (building_id);

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
FROM device_set ds
JOIN building b
    ON b.org_id = ds.org_id
   AND b.site_id IS NULL
   AND b.deleted_at IS NULL
WHERE dsr.device_set_id = ds.id
  AND dsr.zone IS NOT NULL
  AND dsr.zone <> ''
  AND b.name = dsr.zone;

CREATE INDEX idx_device_set_rack_building
    ON device_set_rack(building_id);
