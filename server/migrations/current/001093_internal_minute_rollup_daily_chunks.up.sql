-- requires-shared: 1000
-- Whole-chunk expiry should track the minute rollup's 14-day retention more
-- closely. Only future chunks change; existing chunks expire on their old bounds.
SELECT set_chunk_time_interval('miner_state_snapshot_device_1m', INTERVAL '1 day');
