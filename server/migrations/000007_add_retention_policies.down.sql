-- Remove retention policies

SELECT remove_retention_policy('device_metrics_daily', if_exists => true);
SELECT remove_retention_policy('device_metrics_hourly', if_exists => true);
SELECT remove_retention_policy('device_metrics', if_exists => true);
