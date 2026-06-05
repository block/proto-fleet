-- Narrowing back to VARCHAR(36); rows longer than 36 chars would fail the cast.
ALTER TABLE device ALTER COLUMN device_identifier TYPE VARCHAR(36);
