ALTER TABLE miner_state_snapshots
    ADD COLUMN building_id BIGINT NULL;

CREATE INDEX idx_miner_state_snapshots_org_building_time
    ON miner_state_snapshots(org_id, building_id, time DESC)
    WHERE building_id IS NOT NULL;

-- Large fleets write millions of raw per-device snapshots per day. Keep raw
-- history short for debugging, and serve dashboard history from the compact
-- aggregate below.
SELECT remove_retention_policy('miner_state_snapshots', if_exists => true);
SELECT add_retention_policy('miner_state_snapshots', INTERVAL '14 days',
    schedule_interval => INTERVAL '1 hour');

-- Future chunks should be small enough for retention/compression jobs to work
-- in day-sized units. Existing chunks keep their current interval.
SELECT set_chunk_time_interval('miner_state_snapshots', INTERVAL '1 day');

CREATE MATERIALIZED VIEW miner_state_snapshot_counts_1m
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT
    time_bucket(INTERVAL '1 minute', time) AS bucket,
    time AS snapshot_time,
    org_id,
    site_id,
    building_id,
    COUNT(*) FILTER (WHERE state = 3)::int AS hashing_count,
    COUNT(*) FILTER (WHERE state = 2)::int AS broken_count,
    COUNT(*) FILTER (WHERE state = 0)::int AS offline_count,
    COUNT(*) FILTER (WHERE state = 1)::int AS sleeping_count
FROM miner_state_snapshots
GROUP BY bucket, snapshot_time, org_id, site_id, building_id
WITH NO DATA;

CREATE INDEX idx_miner_state_snapshot_counts_1m_org_bucket
    ON miner_state_snapshot_counts_1m(org_id, bucket DESC, snapshot_time DESC);
CREATE INDEX idx_miner_state_snapshot_counts_1m_org_site_bucket
    ON miner_state_snapshot_counts_1m(org_id, site_id, bucket DESC, snapshot_time DESC);
CREATE INDEX idx_miner_state_snapshot_counts_1m_org_building_bucket
    ON miner_state_snapshot_counts_1m(org_id, building_id, bucket DESC, snapshot_time DESC);

SELECT add_continuous_aggregate_policy('miner_state_snapshot_counts_1m',
    start_offset => INTERVAL '14 days',
    end_offset => INTERVAL '2 minutes',
    schedule_interval => INTERVAL '1 minute');

ALTER MATERIALIZED VIEW miner_state_snapshot_counts_1m SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'org_id, site_id, building_id',
    timescaledb.compress_orderby = 'bucket DESC, snapshot_time DESC'
);

SELECT add_compression_policy('miner_state_snapshot_counts_1m', INTERVAL '7 days',
    schedule_interval => INTERVAL '1 hour');
SELECT add_retention_policy('miner_state_snapshot_counts_1m', INTERVAL '1 year',
    schedule_interval => INTERVAL '1 day');
