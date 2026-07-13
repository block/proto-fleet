SET LOCAL lock_timeout = '1min';

DROP INDEX IF EXISTS idx_notification_metric_sample_metric_org_device_time;
DROP INDEX IF EXISTS idx_notification_metric_sample_metric_org_result_time;
