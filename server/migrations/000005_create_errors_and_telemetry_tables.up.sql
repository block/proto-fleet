-- Proto Fleet PostgreSQL Errors and Telemetry Tables

-- =====================================================
-- Errors table
-- Single source of truth for hardware error incidents
-- =====================================================
CREATE TABLE errors (
    id                 BIGSERIAL PRIMARY KEY,
    error_id           VARCHAR(36) NOT NULL UNIQUE,
    org_id             BIGINT NOT NULL,
    miner_error        INT NOT NULL,
    severity           INT NOT NULL,
    summary            TEXT NOT NULL,
    impact             TEXT,
    cause_summary      TEXT,
    recommended_action TEXT,
    first_seen_at      TIMESTAMPTZ NOT NULL,
    last_seen_at       TIMESTAMPTZ NOT NULL,
    closed_at          TIMESTAMPTZ,

    -- Device/component attribution
    device_id          BIGINT NOT NULL,
    component_id       VARCHAR(255),
    component_type     INT,

    -- Vendor metadata
    vendor_code        VARCHAR(255),
    firmware           VARCHAR(255),
    extra              JSONB,

    created_at         TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_errors_organization FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_errors_device FOREIGN KEY (device_id)
        REFERENCES device(id) ON DELETE CASCADE
);

CREATE INDEX idx_errors_dedup ON errors(org_id, device_id, miner_error, component_id, component_type);
CREATE INDEX idx_errors_org_miner_error ON errors(org_id, miner_error);
CREATE INDEX idx_errors_org_severity ON errors(org_id, severity);
CREATE INDEX idx_errors_org_last_seen ON errors(org_id, last_seen_at DESC);
CREATE INDEX idx_errors_org_component ON errors(org_id, component_id, component_type);
CREATE INDEX idx_errors_open ON errors(org_id, closed_at, severity);
CREATE INDEX idx_errors_pagination ON errors(org_id, last_seen_at DESC, id DESC);

CREATE TRIGGER update_errors_updated_at
    BEFORE UPDATE ON errors
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- Device Metrics table (TimescaleDB Hypertable)
-- Time-series data for device telemetry
-- =====================================================
CREATE TABLE device_metrics (
    time               TIMESTAMPTZ NOT NULL,
    device_identifier  TEXT NOT NULL,
    hash_rate_hs       DOUBLE PRECISION,
    hash_rate_hs_kind  TEXT,
    temp_c             DOUBLE PRECISION,
    temp_c_kind        TEXT,
    fan_rpm            DOUBLE PRECISION,
    fan_rpm_kind       TEXT,
    power_w            DOUBLE PRECISION,
    power_w_kind       TEXT,
    efficiency_jh      DOUBLE PRECISION,
    efficiency_jh_kind TEXT,
    voltage_v          DOUBLE PRECISION,
    voltage_v_kind     TEXT,
    current_a          DOUBLE PRECISION,
    current_a_kind     TEXT,
    inlet_temp_c       DOUBLE PRECISION,
    outlet_temp_c      DOUBLE PRECISION,
    ambient_temp_c     DOUBLE PRECISION,
    chip_count         INTEGER,
    chip_count_kind    TEXT,
    chip_frequency_mhz DOUBLE PRECISION,
    health             TEXT,

    PRIMARY KEY (time, device_identifier)
);

-- Convert to TimescaleDB hypertable with 1-day chunks
SELECT create_hypertable('device_metrics', by_range('time', INTERVAL '1 day'));

CREATE INDEX idx_device_metrics_device_identifier ON device_metrics(device_identifier, time DESC);

-- Enable compression on chunks older than 7 days
ALTER TABLE device_metrics SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'device_identifier',
    timescaledb.compress_orderby = 'time DESC'
);

SELECT add_compression_policy('device_metrics', INTERVAL '7 days');

