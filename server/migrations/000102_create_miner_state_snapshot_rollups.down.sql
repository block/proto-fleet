SELECT remove_retention_policy('miner_state_snapshots', if_exists => true);
SELECT add_retention_policy('miner_state_snapshots', INTERVAL '1 year',
    schedule_interval => INTERVAL '6 hours');

SELECT remove_retention_policy('miner_state_snapshot_daily', if_exists => true);
SELECT remove_compression_policy('miner_state_snapshot_daily', if_exists => true);
DROP TABLE IF EXISTS miner_state_snapshot_daily;

SELECT remove_retention_policy('miner_state_snapshot_hourly', if_exists => true);
SELECT remove_compression_policy('miner_state_snapshot_hourly', if_exists => true);
DROP TABLE IF EXISTS miner_state_snapshot_hourly;
