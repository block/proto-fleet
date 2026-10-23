-- requires-shared: 1000
ALTER TABLE discovered_device
    ADD COLUMN endpoint_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN endpoints JSONB NOT NULL DEFAULT '{}';
CREATE INDEX idx_discovered_device_endpoint_key ON discovered_device (org_id, endpoint_key)
    WHERE deleted_at IS NULL AND endpoint_key <> '';
CREATE TABLE container_ha_observation (
    device_id BIGINT PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
    observed_at TIMESTAMPTZ NOT NULL,
    degraded BOOLEAN NOT NULL
);
