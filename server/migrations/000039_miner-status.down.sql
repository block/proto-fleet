UPDATE device_status 
SET status = CASE 
    WHEN status IN ('UNKNOWN', 'INACTIVE') THEN 'OFFLINE'
    WHEN status = 'ACTIVE' THEN 'ONLINE'
    ELSE status
END
WHERE status IN ('ACTIVE', 'INACTIVE', 'UNKNOWN');

ALTER TABLE device_status
  DROP INDEX ux_device_status_device_id,
  MODIFY COLUMN status ENUM(
    'ONLINE',
    'OFFLINE', 
    'MAINTENANCE',
    'ERROR'
  ) NOT NULL;
