SET LOCAL lock_timeout = '1min';

SELECT set_chunk_time_interval('notification_metric_sample', INTERVAL '1 hour');

SELECT remove_compression_policy('notification_metric_sample', if_exists => true);
SELECT add_compression_policy('notification_metric_sample', INTERVAL '4 hours');

SELECT remove_retention_policy('notification_metric_sample', if_exists => true);
SELECT add_retention_policy('notification_metric_sample', INTERVAL '7 days');
