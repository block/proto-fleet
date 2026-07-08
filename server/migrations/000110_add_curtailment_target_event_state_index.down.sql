-- Online drop; see the up migration's CONCURRENTLY note. Must be the sole
-- statement in the file (no implicit transaction).
DROP INDEX CONCURRENTLY IF EXISTS idx_curtailment_target_event_state;
