DROP INDEX IF EXISTS idx_device_status_ip_recovery_queue;

ALTER TABLE device_status
    DROP COLUMN IF EXISTS ip_recovery_last_dispatched_at;
