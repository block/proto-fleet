UPDATE discovered_device SET is_active = FALSE WHERE is_active IS NULL;

ALTER TABLE discovered_device MODIFY COLUMN is_active BOOLEAN NOT NULL DEFAULT FALSE;
