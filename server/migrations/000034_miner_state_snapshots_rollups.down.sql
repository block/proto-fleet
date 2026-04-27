-- Restore raw retention to 1 year and drop both rollup CAGGs.
SELECT remove_retention_policy('miner_state_snapshots', if_exists => true);
SELECT add_retention_policy('miner_state_snapshots', INTERVAL '1 year');

SELECT remove_retention_policy('miner_state_snapshots_daily', if_exists => true);
SELECT remove_compression_policy('miner_state_snapshots_daily', if_exists => true);
SELECT remove_continuous_aggregate_policy('miner_state_snapshots_daily', if_exists => true);

DROP INDEX IF EXISTS idx_miner_state_snapshots_daily_org_bucket;
DROP MATERIALIZED VIEW IF EXISTS miner_state_snapshots_daily;

SELECT remove_retention_policy('miner_state_snapshots_hourly', if_exists => true);
SELECT remove_compression_policy('miner_state_snapshots_hourly', if_exists => true);
SELECT remove_continuous_aggregate_policy('miner_state_snapshots_hourly', if_exists => true);

DROP INDEX IF EXISTS idx_miner_state_snapshots_hourly_org_bucket;
DROP MATERIALIZED VIEW IF EXISTS miner_state_snapshots_hourly;
