-- Record raw device-identity fields on each command_on_device_log row at
-- command-completion time, so the activity-log detail view can show operators
-- what a miner was called / where it was when the action ran — even if the
-- device is later renamed or moves to a new IP.
--
-- Raw components (not a composed display name) so the read path can derive
-- the name via the same Go helper as the live fleet read path, keeping the
-- two in lockstep.
--
-- Nullable for backward compatibility: historical rows stay NULL and the
-- frontend falls back to the device UUID.

ALTER TABLE command_on_device_log
    ADD COLUMN custom_name  TEXT NULL,
    ADD COLUMN manufacturer TEXT NULL,
    ADD COLUMN model        TEXT NULL,
    ADD COLUMN ip_address   TEXT NULL,
    ADD COLUMN mac_address  TEXT NULL;
