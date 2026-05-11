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

-- ON DELETE CASCADE matches the single-column FK on device_set_id
-- (added in migration 000012); the composite FK adds the org-matching
-- invariant without changing cascade semantics.
ALTER TABLE device_set_rack
    ADD CONSTRAINT fk_device_set_rack_device_set_org FOREIGN KEY (device_set_id, org_id)
        REFERENCES device_set(id, org_id) ON DELETE CASCADE;

ALTER TABLE device_set_rack
    ADD COLUMN building_id BIGINT NULL,
    ADD CONSTRAINT fk_device_set_rack_building FOREIGN KEY (building_id, org_id)
        REFERENCES building(id, org_id) ON DELETE SET NULL (building_id);

CREATE INDEX idx_device_set_rack_building
    ON device_set_rack(building_id);
