-- Tighten notification_metric_sample storage policies.
--
-- The table's only latency-sensitive readers are the Grafana alert rules,
-- which query at most the last 24 hours (ingest-stalled heartbeat join) and
-- typically the last 10-15 minutes. The previous policies kept ~2 days of
-- data uncompressed in row form (with full B-tree indexes) and 30 days
-- total, which at fleet scale meant the indexed representation dominated
-- disk. Compressing after 4 hours keeps the expensive representation to a
-- few hours of data; 7 days of compressed history remains for ad-hoc
-- Explore queries.
--
-- 1-hour chunks (down from 1 day) let the compression policy pick chunks up
-- soon after they close instead of waiting out a day-sized chunk, and keep
-- retention drops small. Applies to newly created chunks only; existing
-- chunks keep their original span and age out under the new retention.
--
-- remove_*_policy blocks on the bgw_job row lock for the duration of a
-- currently-executing policy run (the outgoing job may be mid-compress of a
-- day-sized chunk), so bound the wait like 000120; the file is one implicit
-- transaction, and on timeout `migrate force 120` + rerun recovers cleanly.
SET LOCAL lock_timeout = '1min';

SELECT set_chunk_time_interval('notification_metric_sample', INTERVAL '1 hour');

SELECT remove_compression_policy('notification_metric_sample', if_exists => true);
-- Retry-buffer lag is count-bounded (seconds-to-minutes at fleet scale),
-- and a stale retry into an already-compressed chunk is a supported INSERT
-- on this TimescaleDB (>= 2.11), so 4 hours is safe.
SELECT add_compression_policy('notification_metric_sample', INTERVAL '4 hours');

SELECT remove_retention_policy('notification_metric_sample', if_exists => true);
SELECT add_retention_policy('notification_metric_sample', INTERVAL '7 days');
