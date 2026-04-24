-- Snapshot human-readable device identity on each command_on_device_log row at
-- first write, so the activity log detail view can show name/IP/MAC for audit
-- purposes without resolving against mutable current state (custom_name can be
-- renamed, ip_address can change under DHCP).
--
-- Nullable for backward compatibility: historical rows stay NULL and the
-- frontend falls back to the device UUID.

ALTER TABLE command_on_device_log
    ADD COLUMN device_name TEXT NULL,
    ADD COLUMN ip_address  TEXT NULL,
    ADD COLUMN mac_address TEXT NULL;
