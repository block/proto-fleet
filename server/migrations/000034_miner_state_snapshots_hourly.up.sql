-- Hourly rollup of miner_state_snapshots — one row per (hour, org, device)
-- carrying the last state observed in that hour. Lets us keep only 30 days of
-- per-minute raw history while still serving 90d / 1y chart views cheaply.

CREATE MATERIALIZED VIEW miner_state_snapshots_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket(INTERVAL '1 hour', time) AS bucket,
    org_id,
    device_identifier,
    last(state, time) AS state
FROM miner_state_snapshots
GROUP BY bucket, org_id, device_identifier
WITH NO DATA;

SELECT add_continuous_aggregate_policy('miner_state_snapshots_hourly',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes');

CREATE INDEX idx_miner_state_snapshots_hourly_org_bucket
    ON miner_state_snapshots_hourly (org_id, bucket DESC);

ALTER MATERIALIZED VIEW miner_state_snapshots_hourly SET (
    timescaledb.compress = true,
    timescaledb.compress_segmentby = 'device_identifier',
    timescaledb.compress_orderby = 'bucket DESC'
);

SELECT add_compression_policy('miner_state_snapshots_hourly', INTERVAL '7 days');
SELECT add_retention_policy('miner_state_snapshots_hourly', INTERVAL '1 year');

-- Shrink raw retention to 30 days. The hourly rollup covers anything older.
SELECT remove_retention_policy('miner_state_snapshots', if_exists => true);
SELECT add_retention_policy('miner_state_snapshots', INTERVAL '30 days');
