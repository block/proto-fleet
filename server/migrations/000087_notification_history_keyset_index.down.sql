-- CONCURRENTLY must be the sole statement and cannot run in a transaction (golang-migrate runs it directly).
DROP INDEX CONCURRENTLY IF EXISTS idx_notification_history_org_id;
