ALTER TABLE device_status
  MODIFY COLUMN status ENUM(
    'ACTIVE',
    'INACTIVE',
    'OFFLINE',
    'MAINTENANCE',
    'ERROR',
    'UNKNOWN'
  ) NOT NULL
    DEFAULT 'ACTIVE',
  ADD UNIQUE INDEX ux_device_status_device_id (device_id);
