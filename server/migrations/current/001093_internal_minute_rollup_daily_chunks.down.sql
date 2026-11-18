-- Restores the interval for future chunks without merging existing daily chunks.
SELECT set_chunk_time_interval('miner_state_snapshot_device_1m', INTERVAL '10 days');
