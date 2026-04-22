-- Hourly + daily rollups of miner_state_snapshots — one row per device per
-- bucket carrying the last state observed in that bucket. Lets us keep only
-- 30 days of per-minute raw history while still serving 30d / 90d / 1y chart
-- views cheaply.
--
-- Policies mirror device_metrics_hourly/_daily (000016) and device_status
-- (000008) for consistency with the rest of the codebase's time-series.

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
SELECT add_retention_policy('miner_state_snapshots_hourly', INTERVAL '3 months');

CREATE MATERIALIZED VIEW miner_state_snapshots_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket(INTERVAL '1 day', time) AS bucket,
    org_id,
    device_identifier,
    last(state, time) AS state
FROM miner_state_snapshots
GROUP BY bucket, org_id, device_identifier
WITH NO DATA;

SELECT add_continuous_aggregate_policy('miner_state_snapshots_daily',
    start_offset => INTERVAL '7 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '6 hours');

CREATE INDEX idx_miner_state_snapshots_daily_org_bucket
    ON miner_state_snapshots_daily (org_id, bucket DESC);

ALTER MATERIALIZED VIEW miner_state_snapshots_daily SET (
    timescaledb.compress = true,
    timescaledb.compress_segmentby = 'device_identifier',
    timescaledb.compress_orderby = 'bucket DESC'
);

SELECT add_compression_policy('miner_state_snapshots_daily', INTERVAL '7 days');
SELECT add_retention_policy('miner_state_snapshots_daily', INTERVAL '3 years');

-- Shrink raw retention to 30 days. The hourly and daily rollups cover older
-- history; matches the raw device_metrics retention from migration 000007.
SELECT remove_retention_policy('miner_state_snapshots', if_exists => true);
SELECT add_retention_policy('miner_state_snapshots', INTERVAL '30 days');
