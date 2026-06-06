-- No-op: PostgreSQL cannot mark a validated CHECK constraint as NOT VALID.
-- Migration 000077 down drops these constraints together with the phase columns.
SELECT 1;
