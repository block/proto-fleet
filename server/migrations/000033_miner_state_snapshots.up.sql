CREATE TABLE miner_state_snapshots (
    time              TIMESTAMPTZ NOT NULL,
    org_id            BIGINT      NOT NULL,
    device_identifier TEXT        NOT NULL,
    state             SMALLINT    NOT NULL,

    PRIMARY KEY (time, device_identifier)
);

SELECT create_hypertable('miner_state_snapshots', by_range('time', INTERVAL '7 days'));

CREATE INDEX idx_miner_state_snapshots_org_time
    ON miner_state_snapshots (org_id, time DESC);
CREATE INDEX idx_miner_state_snapshots_device_time
    ON miner_state_snapshots (device_identifier, time DESC);

ALTER TABLE miner_state_snapshots SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'device_identifier',
    timescaledb.compress_orderby = 'time DESC'
);

SELECT add_compression_policy('miner_state_snapshots', INTERVAL '7 days');
SELECT add_retention_policy('miner_state_snapshots', INTERVAL '1 year');
