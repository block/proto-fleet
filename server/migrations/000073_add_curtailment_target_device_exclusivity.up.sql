-- Device-level exclusivity: a device belongs to at most one non-terminal
-- curtailment. Replaces the org-level singleton dropped in 000072 as the guard
-- against double-curtailing a device, so concurrent disjoint-scope events
-- coexist while a race that would overlap a device fails at insert.
-- device_identifier is globally unique (device.uq_device_device_identifier).
--
-- CONCURRENTLY so the build doesn't block curtailment_target writes. golang-
-- migrate v4's postgres driver runs the body via conn.ExecContext with no
-- implicit transaction, so this is safe as the sole statement in the file. A
-- failed build leaves schema_migrations.dirty=true at version 73 and may leave
-- an INVALID index; recovery is to DROP it and `migrate force 72` before
-- re-deploy.
CREATE UNIQUE INDEX CONCURRENTLY uq_curtailment_target_one_non_terminal_per_device
    ON curtailment_target (device_identifier)
    WHERE state NOT IN ('resolved', 'restore_failed', 'released');
