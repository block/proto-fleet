-- App-maintained 90s fleet metric rollups for dashboard all-device reads.
-- site_id = 0 represents unassigned devices; site.id is BIGSERIAL from 1.
CREATE TABLE fleet_metric_rollup_90s (
    bucket TIMESTAMPTZ NOT NULL,
    org_id BIGINT NOT NULL,
    site_id BIGINT NOT NULL DEFAULT 0,

    avg_hash_rate DOUBLE PRECISION,
    min_hash_rate DOUBLE PRECISION,
    max_hash_rate DOUBLE PRECISION,
    latest_hash_rate DOUBLE PRECISION,
    hash_rate_device_count BIGINT NOT NULL DEFAULT 0,

    min_temp DOUBLE PRECISION,
    max_temp DOUBLE PRECISION,
    sum_temp DOUBLE PRECISION,
    temp_points BIGINT NOT NULL DEFAULT 0,
    temp_device_count BIGINT NOT NULL DEFAULT 0,
    temp_cold_count INTEGER NOT NULL DEFAULT 0,
    temp_ok_count INTEGER NOT NULL DEFAULT 0,
    temp_hot_count INTEGER NOT NULL DEFAULT 0,
    temp_critical_count INTEGER NOT NULL DEFAULT 0,

    min_fan_rpm DOUBLE PRECISION,
    max_fan_rpm DOUBLE PRECISION,
    sum_fan_rpm DOUBLE PRECISION,
    fan_rpm_points BIGINT NOT NULL DEFAULT 0,
    fan_rpm_device_count BIGINT NOT NULL DEFAULT 0,

    avg_power DOUBLE PRECISION,
    min_power DOUBLE PRECISION,
    max_power DOUBLE PRECISION,
    latest_power DOUBLE PRECISION,
    power_device_count BIGINT NOT NULL DEFAULT 0,

    min_efficiency DOUBLE PRECISION,
    max_efficiency DOUBLE PRECISION,
    sum_efficiency DOUBLE PRECISION,
    efficiency_points BIGINT NOT NULL DEFAULT 0,
    efficiency_device_count BIGINT NOT NULL DEFAULT 0,

    PRIMARY KEY (org_id, site_id, bucket)
);

CREATE TABLE fleet_metric_rollup_progress (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    earliest_bucket TIMESTAMPTZ NOT NULL,
    latest_bucket TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

SELECT create_hypertable(
    'fleet_metric_rollup_90s',
    by_range('bucket', INTERVAL '1 day'),
    if_not_exists => TRUE
);

CREATE INDEX idx_fleet_metric_rollup_90s_org_bucket
    ON fleet_metric_rollup_90s (org_id, bucket DESC);

-- Keep this aligned with 000104_tighten_device_metrics_retention.
SELECT add_retention_policy('fleet_metric_rollup_90s', INTERVAL '10 days');
