DROP INDEX IF EXISTS idx_device_sort_worker_name;

ALTER TABLE device
DROP COLUMN IF EXISTS worker_name;
