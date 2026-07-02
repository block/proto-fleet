-- No-op: PostgreSQL cannot mark a validated CHECK constraint as NOT VALID.
-- Migration 000106 down swaps these constraints back to the pre-000106 set.
SELECT 1;
