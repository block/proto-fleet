-- Drop tables in reverse order of creation (respecting FK dependencies)
DROP TABLE IF EXISTS rack_slot;
DROP TABLE IF EXISTS device_collection_membership;
DROP TABLE IF EXISTS device_collection_rack;
DROP TRIGGER IF EXISTS update_device_collection_updated_at ON device_collection;
DROP TABLE IF EXISTS device_collection;
DROP TYPE IF EXISTS collection_type;
