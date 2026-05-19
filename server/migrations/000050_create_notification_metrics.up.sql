-- Notifications metrics live in TimescaleDB alongside the rest of the
-- fleet's time-series data. The earlier design pushed them through an
-- otel-collector into VictoriaMetrics; that whole limb has been removed.
-- vmalert now queries the metrics through fleet-api's PromQL shim
-- (server/internal/handlers/promqlshim). The shim accepts only the
-- canonical fleet_alert{rule_id="…"} selector and dispatches by rule_id
-- to the hard-coded SQL statements pinned in that package. vmalert owns
-- scheduling, `for:` debouncing, and dispatch to Alertmanager — running as
-- a separate process makes it the watchdog that catches fleet errors.
--
-- One hypertable per concept matches the contract surface in
-- server/internal/infrastructure/metrics/contract.go. Labels that the
-- contract calls out are columns; everything is partitioned by org so the
-- shim's WHERE organization_id = $1 lookups are cheap.

-- =====================================================
-- Per-device gauges
-- =====================================================
CREATE TABLE notification_device_metrics (
    time             TIMESTAMPTZ NOT NULL,
    organization_id  BIGINT      NOT NULL,
    -- device_id is TEXT because the rest of the fleet identifies devices by
    -- opaque plugin-supplied strings (e.g. "proto-miner-001"). The schema in
    -- device_metrics already uses TEXT for the same reason.
    device_id        TEXT        NOT NULL,
    device_group     TEXT,
    driver           TEXT,

    -- gauges: NULL means the emitter did not record this metric in the
    -- sample. The shim's SQL filters by IS NOT NULL where appropriate.
    online           BOOLEAN,
    hashrate_ths     DOUBLE PRECISION,
    hashrate_expected_ths DOUBLE PRECISION,
    pool_connected   BOOLEAN
);

SELECT create_hypertable('notification_device_metrics', by_range('time', INTERVAL '1 day'));

CREATE INDEX idx_ndm_org_device_time ON notification_device_metrics(organization_id, device_id, time DESC);
CREATE INDEX idx_ndm_org_time         ON notification_device_metrics(organization_id, time DESC);

ALTER TABLE notification_device_metrics SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'organization_id, device_id',
    timescaledb.compress_orderby   = 'time DESC'
);
SELECT add_compression_policy('notification_device_metrics', INTERVAL '7 days');

-- =====================================================
-- Per-device, per-sensor-kind temperature samples
-- =====================================================
CREATE TABLE notification_device_temperature (
    time             TIMESTAMPTZ NOT NULL,
    organization_id  BIGINT      NOT NULL,
    -- device_id is TEXT because the rest of the fleet identifies devices by
    -- opaque plugin-supplied strings (e.g. "proto-miner-001"). The schema in
    -- device_metrics already uses TEXT for the same reason.
    device_id        TEXT        NOT NULL,
    device_group     TEXT,
    driver           TEXT,
    sensor_kind      TEXT        NOT NULL,
    temperature_max_c DOUBLE PRECISION,
    temperature_avg_c DOUBLE PRECISION
);

SELECT create_hypertable('notification_device_temperature', by_range('time', INTERVAL '1 day'));

CREATE INDEX idx_ndt_org_device_kind_time ON notification_device_temperature(organization_id, device_id, sensor_kind, time DESC);
CREATE INDEX idx_ndt_org_time             ON notification_device_temperature(organization_id, time DESC);

ALTER TABLE notification_device_temperature SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'organization_id, device_id, sensor_kind',
    timescaledb.compress_orderby   = 'time DESC'
);
SELECT add_compression_policy('notification_device_temperature', INTERVAL '7 days');

-- =====================================================
-- Command outcome counter — one row per command terminal event
-- =====================================================
CREATE TABLE notification_command_events (
    time             TIMESTAMPTZ NOT NULL,
    organization_id  BIGINT      NOT NULL,
    kind             TEXT        NOT NULL,
    result           TEXT        NOT NULL
);

SELECT create_hypertable('notification_command_events', by_range('time', INTERVAL '1 day'));

CREATE INDEX idx_nce_org_time ON notification_command_events(organization_id, time DESC);

ALTER TABLE notification_command_events SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'organization_id, kind, result',
    timescaledb.compress_orderby   = 'time DESC'
);
SELECT add_compression_policy('notification_command_events', INTERVAL '7 days');

-- =====================================================
-- Telemetry poll outcome counter — one row per poll attempt
-- =====================================================
CREATE TABLE notification_telemetry_poll_events (
    time             TIMESTAMPTZ NOT NULL,
    organization_id  BIGINT      NOT NULL,
    device_id        TEXT,
    result           TEXT        NOT NULL
);

SELECT create_hypertable('notification_telemetry_poll_events', by_range('time', INTERVAL '1 day'));

CREATE INDEX idx_ntpe_org_time ON notification_telemetry_poll_events(organization_id, time DESC);

ALTER TABLE notification_telemetry_poll_events SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'organization_id, result',
    timescaledb.compress_orderby   = 'time DESC'
);
SELECT add_compression_policy('notification_telemetry_poll_events', INTERVAL '7 days');

-- =====================================================
-- device_online_v1: latest online sample per (org, device).
-- Returns one row per known device; online is NULL when the device has
-- never reported. The shim's offline rule reads this view.
-- =====================================================
CREATE VIEW device_online_v1 AS
SELECT DISTINCT ON (organization_id, device_id)
    organization_id,
    device_id,
    device_group,
    driver,
    time         AS last_seen_at,
    online,
    (online IS TRUE AND time >= now() - INTERVAL '10 minutes') AS online_bool
FROM notification_device_metrics
WHERE online IS NOT NULL
ORDER BY organization_id, device_id, time DESC;
