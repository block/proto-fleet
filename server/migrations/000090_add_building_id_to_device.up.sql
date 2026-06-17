-- Adds device.building_id as a direct FK so miners can be assigned to a
-- building independent of rack membership, mirroring device.site_id
-- (migration 000045). The /sites multi-building flow needs a path for
-- "Add miners to building" that doesn't require routing through a rack.
--
-- ON DELETE SET NULL (building_id): PG15+ column list — building
-- deletion only nulls building_id, leaving the NOT NULL org_id intact.
ALTER TABLE device
    ADD COLUMN building_id BIGINT NULL,
    ADD CONSTRAINT fk_device_building FOREIGN KEY (building_id, org_id)
        REFERENCES building(id, org_id) ON DELETE SET NULL (building_id);

CREATE INDEX idx_device_org_building ON device(org_id, building_id);

-- Backfill: seed device.building_id from each device's rack's
-- building_id so existing rack members start in lockstep with the
-- denormalization the new column implies. Without this, every
-- device.building_id would be NULL until the next rack reparent ran the
-- new cascade — leaving an extended window where filter / rollup
-- consumers that read device.building_id would silently undercount.
--
-- Skips soft-deleted devices/racks and racks without a building. Runs
-- after the column + FK are in place but before any consumer can read
-- it (single-migration transaction).
UPDATE device d
SET building_id = dsr.building_id
FROM device_set_membership dsm
JOIN device_set ds
    ON ds.id = dsm.device_set_id
   AND ds.deleted_at IS NULL
JOIN device_set_rack dsr
    ON dsr.device_set_id = dsm.device_set_id
   AND dsr.org_id = dsm.org_id
WHERE d.id = dsm.device_id
  AND d.org_id = dsm.org_id
  AND dsm.device_set_type = 'rack'
  AND d.deleted_at IS NULL
  AND dsr.building_id IS NOT NULL;
