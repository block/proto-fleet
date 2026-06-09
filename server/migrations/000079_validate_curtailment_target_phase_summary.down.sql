-- No-op: PostgreSQL cannot mark a validated CHECK constraint as NOT VALID.
-- Migration 000078 down drops these constraints together with the phase columns.
SELECT 1;
