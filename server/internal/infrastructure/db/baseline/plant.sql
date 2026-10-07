-- Add private plant history without creating or dropping legacy snapshots.
CREATE TABLE container_plant_metrics (
 device_id BIGINT NOT NULL REFERENCES device(id) ON DELETE CASCADE,
 time TIMESTAMPTZ NOT NULL,
 metrics JSONB NOT NULL CHECK (jsonb_typeof(metrics) = 'object'),
 PRIMARY KEY (device_id, time)
);
SELECT create_hypertable('container_plant_metrics', 'time');
SELECT add_retention_policy('container_plant_metrics', INTERVAL '30 days');
