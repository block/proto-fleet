SELECT remove_continuous_aggregate_policy('miner_state_snapshot_device_daily', if_exists => true);
SELECT remove_retention_policy('miner_state_snapshot_device_daily', if_exists => true);
SELECT remove_compression_policy('miner_state_snapshot_device_daily', if_exists => true);
DROP INDEX IF EXISTS idx_miner_state_snapshot_device_daily_org_device_bucket;
DROP INDEX IF EXISTS idx_miner_state_snapshot_device_daily_org_bucket;
DROP MATERIALIZED VIEW IF EXISTS miner_state_snapshot_device_daily;

SELECT remove_continuous_aggregate_policy('miner_state_snapshot_device_hourly', if_exists => true);
SELECT remove_retention_policy('miner_state_snapshot_device_hourly', if_exists => true);
SELECT remove_compression_policy('miner_state_snapshot_device_hourly', if_exists => true);
DROP INDEX IF EXISTS idx_miner_state_snapshot_device_hourly_org_device_bucket;
DROP INDEX IF EXISTS idx_miner_state_snapshot_device_hourly_org_bucket;
DROP MATERIALIZED VIEW IF EXISTS miner_state_snapshot_device_hourly;

SELECT remove_continuous_aggregate_policy('miner_state_snapshot_device_1m', if_exists => true);
SELECT remove_retention_policy('miner_state_snapshot_device_1m', if_exists => true);
SELECT remove_compression_policy('miner_state_snapshot_device_1m', if_exists => true);
DROP INDEX IF EXISTS idx_miner_state_snapshot_device_1m_org_device_bucket;
DROP INDEX IF EXISTS idx_miner_state_snapshot_device_1m_org_bucket;
DROP MATERIALIZED VIEW IF EXISTS miner_state_snapshot_device_1m;

SELECT remove_retention_policy('miner_state_snapshots', if_exists => true);
SELECT add_retention_policy('miner_state_snapshots', INTERVAL '1 year');

SELECT set_chunk_time_interval('miner_state_snapshots', INTERVAL '7 days');
