-- Keep the concurrent drop outside a transaction and as the sole statement.
DROP INDEX CONCURRENTLY IF EXISTS idx_queue_message_pending_created;
