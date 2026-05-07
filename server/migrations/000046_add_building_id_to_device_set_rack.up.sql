-- Denormalize org_id onto device_set_rack so the building FK can be
-- composite-keyed and Postgres rejects cross-tenant rack/building
-- pointers. Backfilled from the parent device_set; composite FK against
-- device_set keeps it in lockstep going forward.
ALTER TABLE device_set_rack ADD COLUMN org_id BIGINT NULL;

UPDATE device_set_rack dsr
SET org_id = ds.org_id
FROM device_set ds
WHERE dsr.device_set_id = ds.id;

ALTER TABLE device_set_rack
    ALTER COLUMN org_id SET NOT NULL;

ALTER TABLE device_set
    ADD CONSTRAINT uq_device_set_id_org_id UNIQUE (id, org_id);

ALTER TABLE device_set_rack
    ADD CONSTRAINT fk_device_set_rack_device_set_org FOREIGN KEY (device_set_id, org_id)
        REFERENCES device_set(id, org_id);

ALTER TABLE device_set_rack
    ADD COLUMN building_id BIGINT NULL,
    ADD CONSTRAINT fk_device_set_rack_building FOREIGN KEY (building_id, org_id)
        REFERENCES building(id, org_id) ON DELETE SET NULL (building_id);

-- Promote each unique non-null zone per org into a building. NOT EXISTS
-- guards idempotency so up/down/up round-trips don't duplicate.
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
