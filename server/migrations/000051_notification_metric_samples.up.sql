-- Notification metric samples
--
-- VictoriaMetrics has been dropped from the notifications stack. Every
-- sample emitted by the in-process metrics provider lands in this
-- hypertable, and Grafana (running as a sidecar) polls it directly to
-- evaluate the alert rules that used to live in vmalert.
--
-- The schema is intentionally narrow: one row per (time, metric,
-- label-set, value) sample. All known contract labels are materialised
-- as columns so the typical query — filter on metric + a couple of
-- labels, bucket by time — does not have to dig into a JSONB blob.
-- Optional labels store '' (the empty string) when unset, which keeps
-- index lookups simple and avoids null-handling in Grafana queries.

CREATE TABLE notification_metric_sample (
    time              TIMESTAMPTZ      NOT NULL,
    metric            TEXT             NOT NULL,
    organization_id   TEXT             NOT NULL DEFAULT '',
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

-- Hot path for both gauge "last value per series" lookups and counter
-- rate() over a (metric, org, device) window. Putting metric first is
-- deliberate: every Grafana query is scoped to a single metric.
CREATE INDEX idx_notification_metric_sample_metric_time
    ON notification_metric_sample (metric, time DESC);

CREATE INDEX idx_notification_metric_sample_metric_org_device_time
    ON notification_metric_sample (metric, organization_id, device_id, time DESC);

-- Counters segment by (metric, result) — e.g. the telemetry-poll failure
-- rate rule reads every row with result='failure' for the last 10m.
CREATE INDEX idx_notification_metric_sample_metric_org_result_time
    ON notification_metric_sample (metric, organization_id, result, time DESC)
    WHERE result <> '';

ALTER TABLE notification_metric_sample SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'metric, organization_id',
    timescaledb.compress_orderby   = 'time DESC'
);

-- Notification samples are noisy and short-lived; compress chunks older
-- than two days and drop them after 30 to bound disk usage.
SELECT add_compression_policy('notification_metric_sample', INTERVAL '2 days');
SELECT add_retention_policy('notification_metric_sample', INTERVAL '30 days');
