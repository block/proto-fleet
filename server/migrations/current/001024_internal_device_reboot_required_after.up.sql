-- requires-shared: 1000
ALTER TABLE device_status ADD COLUMN reboot_required_after TIMESTAMPTZ;

COMMENT ON COLUMN device_status.reboot_required_after IS
    'Earliest time a boot must follow to satisfy the pending firmware installation; NULL falls back to status_timestamp.';
