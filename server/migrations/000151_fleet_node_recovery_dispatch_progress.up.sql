ALTER TABLE device_status
    ADD COLUMN ip_recovery_last_dispatched_at TIMESTAMPTZ;

CREATE INDEX idx_device_status_ip_recovery_queue
    ON device_status (ip_recovery_last_dispatched_at ASC NULLS FIRST, status_timestamp ASC, device_id)
    WHERE status = 'OFFLINE';
