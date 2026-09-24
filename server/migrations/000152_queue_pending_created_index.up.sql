-- GetMessagesToProcess selects the oldest pending messages globally. The
-- existing (device_id, status, created_at) index serves its per-device blocker
-- lookup, but cannot provide the outer query's created_at ordering.
--
-- CONCURRENTLY keeps queue writes available during the build. It must remain
-- the sole statement: the migration runner executes it outside a transaction.
-- If a build fails, inspect and drop any INVALID index before retrying the
-- dirty migration; IF NOT EXISTS would silently accept that unusable index.
CREATE INDEX CONCURRENTLY idx_queue_message_pending_created
    ON queue_message (created_at)
    WHERE status = 'PENDING';
