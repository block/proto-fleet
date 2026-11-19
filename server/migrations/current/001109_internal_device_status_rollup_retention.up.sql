-- requires-shared: 1000
-- Match the corresponding device metrics retention windows. Summary expiration
-- is independent of the telemetry recovery worker and raw data fence.
SELECT add_retention_policy('device_status_hourly', INTERVAL '3 months');
SELECT add_retention_policy('device_status_daily', INTERVAL '3 years');
