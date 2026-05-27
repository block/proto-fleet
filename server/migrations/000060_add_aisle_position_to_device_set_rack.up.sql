-- Position a rack within its parent building's aisle grid. Both
-- columns are NULL when the rack is unassigned, directly under a
-- site, or assigned to a building without a chosen grid cell.
ALTER TABLE device_set_rack
    ADD COLUMN aisle_index INT NULL,
    ADD COLUMN position_in_aisle INT NULL;

-- Non-negative when set. Upper bounds (< building.aisles and
-- < building.racks_per_aisle) are validated in the service layer
-- because they depend on the parent building row.
ALTER TABLE device_set_rack
    ADD CONSTRAINT ck_device_set_rack_aisle_index_nonneg
        CHECK (aisle_index IS NULL OR aisle_index >= 0),
    ADD CONSTRAINT ck_device_set_rack_position_in_aisle_nonneg
        CHECK (position_in_aisle IS NULL OR position_in_aisle >= 0);

-- Both fields must be set together or both NULL — a half-set
-- position would be ambiguous to the UI.
ALTER TABLE device_set_rack
    ADD CONSTRAINT ck_device_set_rack_position_paired
        CHECK (
            (aisle_index IS NULL AND position_in_aisle IS NULL)
            OR (aisle_index IS NOT NULL AND position_in_aisle IS NOT NULL)
        );

-- A position is only meaningful with a parent building. Reject
-- (aisle_index, position_in_aisle) when building_id is NULL — it
-- would otherwise leak stale layout data when a rack is moved
-- out of a building.
ALTER TABLE device_set_rack
    ADD CONSTRAINT ck_device_set_rack_position_requires_building
        CHECK (
            (aisle_index IS NULL AND position_in_aisle IS NULL)
            OR building_id IS NOT NULL
        );

-- One rack per cell within a building. Partial-unique index so
-- NULL positions (multiple racks in a building without grid
-- placement) remain allowed.
CREATE UNIQUE INDEX uk_device_set_rack_building_position
    ON device_set_rack(building_id, aisle_index, position_in_aisle)
    WHERE building_id IS NOT NULL
      AND aisle_index IS NOT NULL
      AND position_in_aisle IS NOT NULL;
