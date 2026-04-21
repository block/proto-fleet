-- Reverse trigger rename
ALTER TRIGGER update_device_set_updated_at ON device_set RENAME TO update_device_collection_updated_at;

-- Reverse constraint renames
ALTER TABLE device_set_membership RENAME CONSTRAINT uk_device_set_device TO uk_collection_device;
ALTER TABLE device_set_membership RENAME CONSTRAINT fk_membership_device_set TO fk_membership_collection;
ALTER TABLE device_set RENAME CONSTRAINT fk_device_set_org TO fk_device_collection_org;

-- Reverse rack_slot column rename
ALTER TABLE rack_slot RENAME COLUMN device_set_id TO collection_id;

-- Reverse membership table and column renames
ALTER TABLE device_set_membership RENAME COLUMN device_set_type TO collection_type;
ALTER TABLE device_set_membership RENAME COLUMN device_set_id TO collection_id;
ALTER TABLE device_set_membership RENAME TO device_collection_membership;

-- Reverse rack extension table and column renames
ALTER TABLE device_set_rack RENAME COLUMN device_set_id TO collection_id;
ALTER TABLE device_set_rack RENAME TO device_collection_rack;

-- Reverse base table rename
ALTER TABLE device_set RENAME TO device_collection;

-- Reverse enum rename
ALTER TYPE device_set_type RENAME TO collection_type;
