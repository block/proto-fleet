-- Drop trigger functions
DROP FUNCTION IF EXISTS update_updated_at_column() CASCADE;
DROP FUNCTION IF EXISTS update_last_seen_column() CASCADE;

-- Drop ENUM types
DROP TYPE IF EXISTS device_command_status_enum;
DROP TYPE IF EXISTS queue_status_enum;
DROP TYPE IF EXISTS batch_status_enum;
DROP TYPE IF EXISTS pairing_status_enum;
DROP TYPE IF EXISTS device_status_enum;

-- Note: TimescaleDB extension is not dropped to avoid data loss
