-- Add a dedicated batch_id column to activity_log so command events can be
-- joined to their command_batch_log without scanning JSONB metadata.
--
-- Two indexes:
--   1. A partial btree for fast lookups of any activity row belonging to a batch.
--   2. A partial unique index scoped to '*.completed' event types, acting as an
--      idempotency guard for the finalizer so a crash-recovery reconciler can
--      safely re-insert without creating duplicate completion rows.

ALTER TABLE activity_log
    ADD COLUMN batch_id TEXT NULL;

CREATE INDEX idx_activity_log_batch_id
    ON activity_log(batch_id)
    WHERE batch_id IS NOT NULL;

CREATE UNIQUE INDEX uq_activity_log_batch_completed
    ON activity_log(batch_id, event_type)
    WHERE batch_id IS NOT NULL AND event_type LIKE '%.completed';
