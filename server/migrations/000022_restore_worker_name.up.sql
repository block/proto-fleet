ALTER TABLE device
ADD COLUMN worker_name VARCHAR(255) NULL;

UPDATE device
SET worker_name = mac_address
WHERE deleted_at IS NULL
  AND worker_name IS NULL
  AND mac_address IS NOT NULL
  AND mac_address != '';

CREATE INDEX idx_device_sort_worker_name
ON device (org_id, worker_name, discovered_device_id)
WHERE deleted_at IS NULL;
