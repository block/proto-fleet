-- Device-level exclusivity: a device belongs to at most one non-terminal
-- curtailment. Installed before 000073 drops the org-level singleton, so the
-- selector/insert race stays guarded throughout the migration. device_identifier
-- is globally unique (device.uq_device_device_identifier). CONCURRENTLY (sole
-- statement, no implicit tx under golang-migrate v4) avoids blocking writes.
CREATE UNIQUE INDEX CONCURRENTLY uq_curtailment_target_one_non_terminal_per_device
    ON curtailment_target (device_identifier)
    WHERE state NOT IN ('resolved', 'restore_failed', 'released');
