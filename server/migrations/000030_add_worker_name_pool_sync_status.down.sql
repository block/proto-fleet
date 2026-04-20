ALTER TABLE device
DROP COLUMN IF EXISTS worker_name_pool_sync_status;

DROP TYPE IF EXISTS worker_name_pool_sync_status_enum;
