-- Adds Stratum protocol per pool so the server can distinguish SV1 from SV2
-- on every pool read. The DEFAULT 'sv1' covers fresh inserts before the
-- application catches up; existing rows are backfilled below from the
-- URL scheme so a pool with a stratum2+tcp:// URL doesn't get silently
-- coerced to SV1 by the default. Handlers normalize UNSPECIFIED to 'sv1'
-- on insert, so the DB never stores it.
--
-- The CHECK constraint keeps the value space closed — future protocols
-- (e.g. SV3) require a migration rather than a silent proto-level change.
ALTER TABLE pool
    ADD COLUMN protocol TEXT NOT NULL DEFAULT 'sv1'
    CHECK (protocol IN ('sv1', 'sv2'));

-- Backfill existing rows from the URL scheme. Anything not starting with
-- stratum2+tcp:// stays at the 'sv1' default — including malformed or
-- legacy URLs without a stratum scheme, which the runtime treats as
-- SV1-routable today and which would have been read as SV1 before this
-- migration.
UPDATE pool
SET protocol = 'sv2'
WHERE LOWER(url) LIKE 'stratum2+tcp://%';
