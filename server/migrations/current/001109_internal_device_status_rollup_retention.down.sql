-- Remove the policies. Data removed by earlier policy runs cannot be restored.
SELECT remove_retention_policy('device_status_daily', if_exists => true);
SELECT remove_retention_policy('device_status_hourly', if_exists => true);
