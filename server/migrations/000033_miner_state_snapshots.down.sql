SELECT remove_retention_policy('miner_state_snapshots', if_exists => true);
SELECT remove_compression_policy('miner_state_snapshots', if_exists => true);

DROP INDEX IF EXISTS idx_miner_state_snapshots_device_time;
DROP INDEX IF EXISTS idx_miner_state_snapshots_org_time;
DROP TABLE IF EXISTS miner_state_snapshots;
