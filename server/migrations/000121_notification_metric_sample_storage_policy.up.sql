-- Alert rules read at most the last 24h, so keep the expensive uncompressed
-- indexed representation to hours, not days (full rationale in PR #739).

-- Bound waits on the bgw_job row lock held by an in-flight policy run;
-- on timeout, `migrate force 120` + rerun recovers.
SET LOCAL lock_timeout = '1min';

-- 1-hour chunks (new chunks only) let compression pick chunks up soon after
-- they close and keep retention drops small.
SELECT set_chunk_time_interval('notification_metric_sample', INTERVAL '1 hour');

SELECT remove_compression_policy('notification_metric_sample', if_exists => true);
-- A stale retry into a compressed chunk is a supported INSERT on TSDB >= 2.11.
SELECT add_compression_policy('notification_metric_sample', INTERVAL '4 hours');

SELECT remove_retention_policy('notification_metric_sample', if_exists => true);
SELECT add_retention_policy('notification_metric_sample', INTERVAL '7 days');
