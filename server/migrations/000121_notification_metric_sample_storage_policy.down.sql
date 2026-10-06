SELECT set_chunk_time_interval('notification_metric_sample', INTERVAL '1 day');

SELECT remove_compression_policy('notification_metric_sample', if_exists => true);
SELECT add_compression_policy('notification_metric_sample', INTERVAL '2 days');

SELECT remove_retention_policy('notification_metric_sample', if_exists => true);
SELECT add_retention_policy('notification_metric_sample', INTERVAL '30 days');
