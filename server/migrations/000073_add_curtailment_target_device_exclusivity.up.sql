-- Device-level exclusivity: a device belongs to at most one non-terminal
-- curtailment. Replaces the org-level singleton (dropped in 000072) as the
-- guard against double-curtailing a device, so concurrent disjoint-scope events
-- coexist while a race overlapping a device fails at insert. device_identifier
-- is globally unique (device.uq_device_device_identifier). CONCURRENTLY (sole
-- statement, no implicit tx under golang-migrate v4) avoids blocking writes.
CREATE UNIQUE INDEX CONCURRENTLY uq_curtailment_target_one_non_terminal_per_device
    ON curtailment_target (device_identifier)
    WHERE state NOT IN ('resolved', 'restore_failed', 'released');
