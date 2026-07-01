-- Supports the aggregate creation-bucket guard in telemetry queries, which
-- probes for earlier device rows (including soft-deleted) by identifier.
-- The existing uq_device_device_identifier index is partial (deleted_at IS
-- NULL) and cannot serve lookups across deleted rows.
CREATE INDEX idx_device_identifier_created ON device (device_identifier, created_at);
