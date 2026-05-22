SELECT remove_retention_policy('notification_metric_sample', if_exists => true);
SELECT remove_compression_policy('notification_metric_sample', if_exists => true);

DROP TABLE IF EXISTS notification_metric_sample CASCADE;
