-- A controller remains a paired device for connection/credential management.
-- Its container observation is separate from miner inventory and placement.
CREATE TABLE container_snapshot (
    device_id BIGINT PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
    observed_at TIMESTAMPTZ NOT NULL,
    snapshot JSONB NOT NULL CHECK (jsonb_typeof(snapshot) = 'object')
);

-- Only aggregate plant history is retained. Module observations remain latest-only.
CREATE TABLE container_plant_metrics (
 device_id BIGINT NOT NULL REFERENCES device(id) ON DELETE CASCADE,
 time TIMESTAMPTZ NOT NULL,
 metrics JSONB NOT NULL CHECK (jsonb_typeof(metrics) = 'object'),
 PRIMARY KEY (device_id, time)
);
SELECT create_hypertable('container_plant_metrics', 'time');
SELECT add_retention_policy('container_plant_metrics', INTERVAL '30 days');
