-- Drop the unscoped external-reference partial unique index ahead of
-- the non-terminal-scoped recreate in 000059. See 000056.up.sql for
-- the multi-statement / CONCURRENTLY rationale and the recovery shape
-- if the paired CREATE migration fails (same pattern; substitute
-- `external_ref` for `idempotency` and the `(org_id, external_source,
-- external_reference)` tuple).
DROP INDEX CONCURRENTLY IF EXISTS uq_curtailment_event_external_ref;
