-- Bound the uncompressed hot set for high-cardinality telemetry ingest.
--
-- Timescale compression policies evaluate chunk end time, not row time. With a
-- 1-day chunk and compress_after=6h, today's chunk is not eligible until roughly
-- 30 hours after it starts. Smaller chunks let the existing 6h compression
-- window take effect while preserving ingestion cadence and query semantics.
SELECT set_chunk_time_interval('device_metrics', INTERVAL '1 hour');
SELECT set_chunk_time_interval('miner_state_snapshots', INTERVAL '6 hours');

SELECT remove_compression_policy('device_metrics', if_exists => true);
SELECT add_compression_policy('device_metrics', INTERVAL '6 hours',
    schedule_interval => INTERVAL '1 hour');

SELECT remove_compression_policy('miner_state_snapshots', if_exists => true);
SELECT add_compression_policy('miner_state_snapshots', INTERVAL '6 hours',
    schedule_interval => INTERVAL '1 hour');
