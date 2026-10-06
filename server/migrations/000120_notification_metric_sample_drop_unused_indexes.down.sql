CREATE INDEX IF NOT EXISTS idx_notification_metric_sample_metric_org_device_time
    ON notification_metric_sample (metric, organization_id, device_id, time DESC);

CREATE INDEX IF NOT EXISTS idx_notification_metric_sample_metric_org_result_time
    ON notification_metric_sample (metric, organization_id, result, time DESC)
    WHERE result <> '';
