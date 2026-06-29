CREATE TABLE miner_state_snapshot_hourly (
    bucket            TIMESTAMPTZ NOT NULL,
    sample_time       TIMESTAMPTZ NOT NULL,
    org_id            BIGINT      NOT NULL,
    site_id           BIGINT,
    device_identifier TEXT        NOT NULL,
    state             SMALLINT    NOT NULL,

    PRIMARY KEY (bucket, device_identifier)
);

SELECT create_hypertable('miner_state_snapshot_hourly', by_range('bucket', INTERVAL '7 days'));

CREATE INDEX idx_miner_state_snapshot_hourly_org_bucket
    ON miner_state_snapshot_hourly (org_id, bucket DESC);
CREATE INDEX idx_miner_state_snapshot_hourly_org_site_bucket
    ON miner_state_snapshot_hourly (org_id, site_id, bucket DESC);

ALTER TABLE miner_state_snapshot_hourly SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'device_identifier',
    timescaledb.compress_orderby = 'bucket DESC'
);

INSERT INTO miner_state_snapshot_hourly (
    bucket,
    sample_time,
    org_id,
    site_id,
    device_identifier,
    state
)
SELECT bucket, sample_time, org_id, site_id, device_identifier, state
FROM (
    SELECT DISTINCT ON (time_bucket('1 hour', time), device_identifier)
        time_bucket('1 hour', time)::timestamptz AS bucket,
        time AS sample_time,
        org_id,
        site_id,
        device_identifier,
        state
    FROM miner_state_snapshots
    WHERE time >= now() - INTERVAL '14 days'
    ORDER BY time_bucket('1 hour', time), device_identifier, time DESC
) latest
ON CONFLICT (bucket, device_identifier) DO UPDATE SET
    sample_time = EXCLUDED.sample_time,
    org_id = EXCLUDED.org_id,
    site_id = EXCLUDED.site_id,
    state = EXCLUDED.state
WHERE miner_state_snapshot_hourly.sample_time <= EXCLUDED.sample_time;

SELECT add_compression_policy('miner_state_snapshot_hourly', INTERVAL '7 days',
    schedule_interval => INTERVAL '1 hour');
SELECT add_retention_policy('miner_state_snapshot_hourly', INTERVAL '14 days',
    schedule_interval => INTERVAL '6 hours');

CREATE TABLE miner_state_snapshot_daily (
    bucket            TIMESTAMPTZ NOT NULL,
    sample_time       TIMESTAMPTZ NOT NULL,
    org_id            BIGINT      NOT NULL,
    site_id           BIGINT,
    device_identifier TEXT        NOT NULL,
    state             SMALLINT    NOT NULL,

    PRIMARY KEY (bucket, device_identifier)
);

SELECT create_hypertable('miner_state_snapshot_daily', by_range('bucket', INTERVAL '30 days'));

CREATE INDEX idx_miner_state_snapshot_daily_org_bucket
    ON miner_state_snapshot_daily (org_id, bucket DESC);
CREATE INDEX idx_miner_state_snapshot_daily_org_site_bucket
    ON miner_state_snapshot_daily (org_id, site_id, bucket DESC);

ALTER TABLE miner_state_snapshot_daily SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'device_identifier',
    timescaledb.compress_orderby = 'bucket DESC'
);

INSERT INTO miner_state_snapshot_daily (
    bucket,
    sample_time,
    org_id,
    site_id,
    device_identifier,
    state
)
SELECT bucket, sample_time, org_id, site_id, device_identifier, state
FROM (
    SELECT DISTINCT ON (time_bucket('1 day', time), device_identifier)
        time_bucket('1 day', time)::timestamptz AS bucket,
        time AS sample_time,
        org_id,
        site_id,
        device_identifier,
        state
    FROM miner_state_snapshots
    WHERE time >= now() - INTERVAL '400 days'
    ORDER BY time_bucket('1 day', time), device_identifier, time DESC
) latest
ON CONFLICT (bucket, device_identifier) DO UPDATE SET
    sample_time = EXCLUDED.sample_time,
    org_id = EXCLUDED.org_id,
    site_id = EXCLUDED.site_id,
    state = EXCLUDED.state
WHERE miner_state_snapshot_daily.sample_time <= EXCLUDED.sample_time;

SELECT add_compression_policy('miner_state_snapshot_daily', INTERVAL '7 days',
    schedule_interval => INTERVAL '1 hour');
SELECT add_retention_policy('miner_state_snapshot_daily', INTERVAL '400 days',
    schedule_interval => INTERVAL '6 hours');

SELECT remove_retention_policy('miner_state_snapshots', if_exists => true);
SELECT add_retention_policy('miner_state_snapshots', INTERVAL '3 days',
    schedule_interval => INTERVAL '6 hours');
