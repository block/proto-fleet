-- Widen discovered_device.url_scheme to match the gateway proto's advertised
-- limit (fleetnodegateway.v1 ReportDiscoveredDevices: url_scheme max_len = 32).
--
-- The column was VARCHAR(10), but plugins legitimately report non-http schemes
-- longer than 10 chars (e.g. "stratum+tcp" = 11). proto-validate accepts up to
-- 32, so anything 11-32 chars passed validation and then failed the upsert with
-- a value-too-long error, rejecting the whole discovery batch as an internal
-- error. Aligning the column to the advertised length closes that gap.
--
-- Increasing a varchar length modifier does not rewrite the table or its
-- indexes (idx_discovered_device_ip references this column), so this is safe to
-- run online.
ALTER TABLE discovered_device
    ALTER COLUMN url_scheme TYPE VARCHAR(32);
