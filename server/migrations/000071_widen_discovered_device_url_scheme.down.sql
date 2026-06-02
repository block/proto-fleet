-- Revert url_scheme back to VARCHAR(10). Narrowing the column will fail if any
-- existing row stores a scheme longer than 10 chars; that is the expected
-- contract for rolling back this change.
ALTER TABLE discovered_device
    ALTER COLUMN url_scheme TYPE VARCHAR(10);
