-- Rename collection_type enum to device_set_type
ALTER TYPE collection_type RENAME TO device_set_type;

-- Rename base table
ALTER TABLE device_collection RENAME TO device_set;

-- Rename rack extension table and its column
ALTER TABLE device_collection_rack RENAME TO device_set_rack;
ALTER TABLE device_set_rack RENAME COLUMN collection_id TO device_set_id;

-- Rename membership table and its columns
ALTER TABLE device_collection_membership RENAME TO device_set_membership;
ALTER TABLE device_set_membership RENAME COLUMN collection_id TO device_set_id;
ALTER TABLE device_set_membership RENAME COLUMN collection_type TO device_set_type;

-- Rename rack_slot column
ALTER TABLE rack_slot RENAME COLUMN collection_id TO device_set_id;

-- Rename constraints on device_set (was device_collection)
ALTER TABLE device_set RENAME CONSTRAINT fk_device_collection_org TO fk_device_set_org;

-- Rename constraints on device_set_membership (was device_collection_membership)
ALTER TABLE device_set_membership RENAME CONSTRAINT fk_membership_collection TO fk_membership_device_set;
ALTER TABLE device_set_membership RENAME CONSTRAINT uk_collection_device TO uk_device_set_device;

-- Rename trigger
ALTER TRIGGER update_device_collection_updated_at ON device_set RENAME TO update_device_set_updated_at;
