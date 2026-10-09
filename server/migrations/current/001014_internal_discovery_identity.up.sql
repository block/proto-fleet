-- requires-shared: 1000

-- Keep the identity reported by discovery through database reloads before
-- credential-bearing pairing. Existing rows have no retained discovery evidence;
-- do not infer it from a previously paired device at the same endpoint.
ALTER TABLE discovered_device
    ADD COLUMN serial_number TEXT NOT NULL DEFAULT '',
    ADD COLUMN mac_address TEXT NOT NULL DEFAULT '';
