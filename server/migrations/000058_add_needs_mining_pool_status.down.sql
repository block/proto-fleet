-- Convert any NEEDS_MINING_POOL status to ACTIVE before removing the enum value
UPDATE device_status SET status = 'ACTIVE' WHERE status = 'NEEDS_MINING_POOL';

ALTER TABLE device_status
  MODIFY COLUMN status ENUM(
    'ACTIVE',
    'INACTIVE',
    'OFFLINE',
    'MAINTENANCE',
    'ERROR',
    'UNKNOWN'
  ) NOT NULL
    DEFAULT 'ACTIVE';
