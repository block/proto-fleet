-- Keep raw retention unchanged in this migration. The compact rollups are
-- populated by TimescaleDB policies outside the migration transaction, and raw
-- snapshots remain the correctness fallback while the deployment proves rollup
-- coverage in production.
--
-- Policy window design: the rollups are created WITH NO DATA and the writer
-- only inserts at "now", so a region is materialized only if a refresh window
-- ever covers it. Each start_offset therefore spans the full range its rollup
-- serves (query source selection is duration-based, so historical ranges
-- within retention hit the rollup), bounded by retention and the 1-year raw
-- retention. The first policy runs backfill that history in day/week/month
-- batches, newest first. Compression is configured only where compress_after
-- can exceed the refresh window (daily); inside the window it forces refreshes
-- to write into compressed chunks, and older TimescaleDB versions reject the
-- overlap outright.
SELECT set_chunk_time_interval('miner_state_snapshots', INTERVAL '1 day');

CREATE MATERIALIZED VIEW miner_state_snapshot_device_1m
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT
    time_bucket(INTERVAL '1 minute', time) AS bucket,
    org_id,
    device_identifier,
    last(time, time)::timestamptz AS state_time,
    last(state, time)::smallint AS state
FROM miner_state_snapshots
GROUP BY bucket, org_id, device_identifier
WITH NO DATA;

CREATE INDEX idx_miner_state_snapshot_device_1m_org_bucket
    ON miner_state_snapshot_device_1m(org_id, bucket DESC);
CREATE INDEX idx_miner_state_snapshot_device_1m_org_device_bucket
    ON miner_state_snapshot_device_1m(org_id, device_identifier, bucket DESC);

SELECT add_continuous_aggregate_policy('miner_state_snapshot_device_1m',
    start_offset => INTERVAL '14 days',
    end_offset => INTERVAL '2 minutes',
    schedule_interval => INTERVAL '5 minutes',
    buckets_per_batch => 1440);

SELECT add_retention_policy('miner_state_snapshot_device_1m', INTERVAL '14 days',
    schedule_interval => INTERVAL '1 day');

CREATE MATERIALIZED VIEW miner_state_snapshot_device_hourly
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT
    time_bucket(INTERVAL '1 hour', time) AS bucket,
    org_id,
    device_identifier,
    last(state, time)::smallint AS state
FROM miner_state_snapshots
GROUP BY bucket, org_id, device_identifier
WITH NO DATA;

CREATE INDEX idx_miner_state_snapshot_device_hourly_org_bucket
    ON miner_state_snapshot_device_hourly(org_id, bucket DESC);
CREATE INDEX idx_miner_state_snapshot_device_hourly_org_device_bucket
    ON miner_state_snapshot_device_hourly(org_id, device_identifier, bucket DESC);

SELECT add_continuous_aggregate_policy('miner_state_snapshot_device_hourly',
    start_offset => INTERVAL '3 months',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes',
    buckets_per_batch => 168);

SELECT add_retention_policy('miner_state_snapshot_device_hourly', INTERVAL '3 months',
    schedule_interval => INTERVAL '1 day');

CREATE MATERIALIZED VIEW miner_state_snapshot_device_daily
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT
    time_bucket(INTERVAL '1 day', time) AS bucket,
    org_id,
    device_identifier,
    last(state, time)::smallint AS state
FROM miner_state_snapshots
GROUP BY bucket, org_id, device_identifier
WITH NO DATA;

CREATE INDEX idx_miner_state_snapshot_device_daily_org_bucket
    ON miner_state_snapshot_device_daily(org_id, bucket DESC);
CREATE INDEX idx_miner_state_snapshot_device_daily_org_device_bucket
    ON miner_state_snapshot_device_daily(org_id, device_identifier, bucket DESC);

SELECT add_continuous_aggregate_policy('miner_state_snapshot_device_daily',
    start_offset => INTERVAL '12 months',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '6 hours',
    buckets_per_batch => 30);

ALTER MATERIALIZED VIEW miner_state_snapshot_device_daily SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'org_id, device_identifier',
    timescaledb.compress_orderby = 'bucket DESC'
);

SELECT add_compression_policy('miner_state_snapshot_device_daily', INTERVAL '13 months',
    schedule_interval => INTERVAL '1 day');
SELECT add_retention_policy('miner_state_snapshot_device_daily', INTERVAL '3 years',
    schedule_interval => INTERVAL '1 week');
