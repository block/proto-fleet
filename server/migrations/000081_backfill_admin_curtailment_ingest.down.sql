-- Reverses 000081_backfill_admin_curtailment_ingest.up.sql by removing
-- curtailment:ingest from active ADMIN roles. The permission row itself is
-- owned by 000062 and is intentionally left in place.
--
-- Rolling back this data migration cleanly is impossible without provenance
-- tracking; the rollback path is rare/dev-only and assumes no operator has
-- intentionally hand-granted this key to ADMIN before rollback.

DELETE FROM role_permission
WHERE permission_id = (
    SELECT id FROM permission WHERE key = 'curtailment:ingest'
)
AND role_id IN (
    SELECT id FROM role WHERE builtin_key = 'ADMIN' AND deleted_at IS NULL
);
