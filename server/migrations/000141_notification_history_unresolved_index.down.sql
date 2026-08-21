-- CONCURRENTLY avoids blocking alert ingestion and must remain the sole statement in this migration.
DROP INDEX CONCURRENTLY IF EXISTS idx_notification_history_org_unresolved_id;
