-- No-op: PostgreSQL cannot mark a validated CHECK constraint as NOT VALID.
-- Migration 000108 down swaps these constraints back to the pre-000108 set.
SELECT 1;
