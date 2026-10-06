SELECT set_chunk_time_interval('device_metrics', INTERVAL '1 day');
SELECT set_chunk_time_interval('miner_state_snapshots', INTERVAL '1 day');

SELECT remove_compression_policy('device_metrics', if_exists => true);
SELECT add_compression_policy('device_metrics', INTERVAL '6 hours',
    schedule_interval => INTERVAL '1 hour');

SELECT remove_compression_policy('miner_state_snapshots', if_exists => true);
SELECT add_compression_policy('miner_state_snapshots', INTERVAL '6 hours',
    schedule_interval => INTERVAL '1 hour');
