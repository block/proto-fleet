-- Notification metric samples

CREATE TABLE notification_metric_sample (
    time              TIMESTAMPTZ      NOT NULL,
    metric            TEXT             NOT NULL,
    organization_id   TEXT             NOT NULL DEFAULT '',
    site_id           TEXT             NOT NULL DEFAULT '',
    device_id         TEXT             NOT NULL DEFAULT '',
    device_group      TEXT             NOT NULL DEFAULT '',
    driver            TEXT             NOT NULL DEFAULT '',
    sensor_kind       TEXT             NOT NULL DEFAULT '',
    kind              TEXT             NOT NULL DEFAULT '',
    result            TEXT             NOT NULL DEFAULT '',
    value             DOUBLE PRECISION NOT NULL
);

SELECT create_hypertable(
    'notification_metric_sample',
    by_range('time', INTERVAL '1 day')
);

CREATE INDEX idx_notification_metric_sample_metric_time
    ON notification_metric_sample (metric, time DESC);

CREATE INDEX idx_notification_metric_sample_metric_org_device_time
    ON notification_metric_sample (metric, organization_id, device_id, time DESC);

CREATE INDEX idx_notification_metric_sample_metric_org_result_time
    ON notification_metric_sample (metric, organization_id, result, time DESC)
    WHERE result <> '';

ALTER TABLE notification_metric_sample SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'metric, organization_id',
    timescaledb.compress_orderby   = 'time DESC'
);

SELECT add_compression_policy('notification_metric_sample', INTERVAL '2 days');
SELECT add_retention_policy('notification_metric_sample', INTERVAL '30 days');
