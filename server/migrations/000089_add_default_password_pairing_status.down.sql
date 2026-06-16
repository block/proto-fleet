UPDATE device_pairing
SET pairing_status = 'AUTHENTICATION_NEEDED'
WHERE pairing_status = 'DEFAULT_PASSWORD';

-- Postgres does not support removing values from an enum type.
-- 'DEFAULT_PASSWORD' is safe to leave in place; the rollback backfill above
-- keeps old application code from silently dropping those devices.
