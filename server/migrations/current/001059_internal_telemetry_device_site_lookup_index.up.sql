-- requires-shared: 1000

-- Match telemetry's site lookup, including soft-deleted device history.
CREATE INDEX idx_device_telemetry_site_lookup
    ON device (device_identifier, (deleted_at IS NULL) DESC, updated_at DESC, id DESC)
    INCLUDE (site_id);
