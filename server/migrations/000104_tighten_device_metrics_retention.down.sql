SELECT remove_retention_policy('device_metrics', if_exists => true);
SELECT add_retention_policy('device_metrics', INTERVAL '30 days');
