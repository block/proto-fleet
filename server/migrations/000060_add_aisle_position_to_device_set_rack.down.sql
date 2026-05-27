DROP INDEX IF EXISTS uk_device_set_rack_building_position;

ALTER TABLE device_set_rack
    DROP CONSTRAINT IF EXISTS ck_device_set_rack_position_requires_building,
    DROP CONSTRAINT IF EXISTS ck_device_set_rack_position_paired,
    DROP CONSTRAINT IF EXISTS ck_device_set_rack_position_in_aisle_nonneg,
    DROP CONSTRAINT IF EXISTS ck_device_set_rack_aisle_index_nonneg;

ALTER TABLE device_set_rack
    DROP COLUMN IF EXISTS position_in_aisle,
    DROP COLUMN IF EXISTS aisle_index;
