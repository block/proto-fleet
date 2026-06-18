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

-- No in-migration backfill, mirroring the site_id peer (000045) which
-- also added device.site_id with no backfill. device.building_id has no
-- read consumer yet — every reference today is a cascade write — so the
-- go-forward cascade keeps it in lockstep on the next rack/miner
-- reparent. Seeding the whole device table here would mean a full-table
-- UPDATE under ACCESS EXCLUSIVE inside the schema-change transaction for
-- no current benefit.
