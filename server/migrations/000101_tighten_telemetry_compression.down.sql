SELECT remove_compression_policy('device_metrics', if_exists => true);
SELECT add_compression_policy('device_metrics', INTERVAL '7 days');

SELECT remove_compression_policy('miner_state_snapshots', if_exists => true);
SELECT add_compression_policy('miner_state_snapshots', INTERVAL '7 days');
