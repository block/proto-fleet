SELECT remove_continuous_aggregate_policy('miner_state_snapshot_counts_1m', if_exists => true);
SELECT remove_retention_policy('miner_state_snapshot_counts_1m', if_exists => true);
SELECT remove_compression_policy('miner_state_snapshot_counts_1m', if_exists => true);

DROP INDEX IF EXISTS idx_miner_state_snapshot_counts_1m_org_building_bucket;
DROP INDEX IF EXISTS idx_miner_state_snapshot_counts_1m_org_site_bucket;
DROP INDEX IF EXISTS idx_miner_state_snapshot_counts_1m_org_bucket;
DROP MATERIALIZED VIEW IF EXISTS miner_state_snapshot_counts_1m;

DROP INDEX IF EXISTS idx_miner_state_snapshots_org_building_time;
ALTER TABLE miner_state_snapshots
    DROP COLUMN IF EXISTS building_id;

SELECT remove_retention_policy('miner_state_snapshots', if_exists => true);
SELECT add_retention_policy('miner_state_snapshots', INTERVAL '1 year');

SELECT set_chunk_time_interval('miner_state_snapshots', INTERVAL '7 days');
