-- Add a dedicated batch_id column to activity_log so command events can be
-- joined to their command_batch_log without scanning JSONB metadata.
--
-- Two indexes:
--   1. A partial btree for fast lookups of any activity row belonging to a batch.
--   2. A partial unique index scoped to '*.completed' event types, acting as an
--      idempotency guard for the finalizer so a crash-recovery reconciler can
--      safely re-insert without creating duplicate completion rows.
--
-- OPERATIONAL NOTE: CREATE INDEX (without CONCURRENTLY) acquires an
-- ACCESS EXCLUSIVE lock on activity_log while the build runs. The partial
-- predicate (batch_id IS NOT NULL) filters out all existing rows, so the
-- index payload is small, but Postgres still scans the full table to
-- evaluate the predicate. On activity_log tables well below the 1-year
-- retention ceiling this is effectively instant; for operators running
-- fleets with larger activity_log tables, run this migration during a
-- low-traffic window. Switching to CREATE INDEX CONCURRENTLY is tracked
-- as a follow-up (requires migrate-tool plumbing to split off the DDL
-- from the wrapping transaction).

ALTER TABLE activity_log
    ADD COLUMN batch_id TEXT NULL;

CREATE INDEX idx_activity_log_batch_id
    ON activity_log(batch_id)
    WHERE batch_id IS NOT NULL;

CREATE UNIQUE INDEX uq_activity_log_batch_completed
    ON activity_log(batch_id, event_type)
    WHERE batch_id IS NOT NULL AND event_type LIKE '%.completed';
