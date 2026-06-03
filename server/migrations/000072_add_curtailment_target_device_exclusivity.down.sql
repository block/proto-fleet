-- Online drop; see the up migration's CONCURRENTLY note. Must be the sole
-- statement in the file (no implicit transaction).
DROP INDEX CONCURRENTLY IF EXISTS uq_curtailment_target_one_non_terminal_per_device;
