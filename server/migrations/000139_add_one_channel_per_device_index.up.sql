-- Device channel exclusivity on the populated membership table. CONCURRENTLY
-- must be the sole statement in this file; golang-migrate's PostgreSQL driver
-- executes it without an implicit transaction. Do not use IF NOT EXISTS:
-- a failed concurrent build can leave an invalid index that must not be skipped.
CREATE UNIQUE INDEX CONCURRENTLY idx_one_channel_per_device
    ON device_set_membership(device_id)
    WHERE device_set_type = 'channel';
