ALTER TABLE command_on_device_log
    DROP COLUMN IF EXISTS custom_name,
    DROP COLUMN IF EXISTS manufacturer,
    DROP COLUMN IF EXISTS model,
    DROP COLUMN IF EXISTS ip_address,
    DROP COLUMN IF EXISTS mac_address;
