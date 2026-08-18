-- Online drop; must remain the sole statement in this migration.
DROP INDEX CONCURRENTLY IF EXISTS idx_one_channel_per_device;
