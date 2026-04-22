-- Links command events to their command_batch_log.uuid without scanning
-- JSONB metadata. The partial btree speeds lookups; the partial unique
-- index guarantees at most one '*.completed' row per batch so finalizer
-- retries stay idempotent.
--
-- CREATE INDEX (without CONCURRENTLY) takes ACCESS EXCLUSIVE on activity_log
-- during the build. The partial predicate keeps the index small but Postgres
-- still scans the table. For large activity_log tables, run during a
-- low-traffic window. Switching to CONCURRENTLY is tracked as a follow-up.

ALTER TABLE activity_log
    ADD COLUMN batch_id TEXT NULL;

CREATE INDEX idx_activity_log_batch_id
    ON activity_log(batch_id)
    WHERE batch_id IS NOT NULL;

CREATE UNIQUE INDEX uq_activity_log_batch_completed
    ON activity_log(batch_id, event_type)
    WHERE batch_id IS NOT NULL AND event_type LIKE '%.completed';
