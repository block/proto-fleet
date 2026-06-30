SELECT remove_retention_policy('device_metrics', if_exists => true);
SELECT add_retention_policy('device_metrics', INTERVAL '10 days',
    schedule_interval => INTERVAL '1 hour');
