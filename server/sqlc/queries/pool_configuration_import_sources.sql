-- name: LockLegacyPoolLibraryForImport :exec
-- A maintenance import must also exclude new rows from the legacy library.
LOCK TABLE pool IN SHARE MODE;

-- name: ListHostedPoolImportSources :many
SELECT r.revision_id, r.number, r.name, r.slots,
 CASE WHEN a.depth = 0 THEN 0 ELSE a.id END::bigint AS owner_account_id
FROM hosted_account_pool_revision r
JOIN hosted_account_pool p ON p.pool_id = r.pool_id AND p.organization_id = r.organization_id AND p.account_id = r.account_id
JOIN hosted_account a ON a.id = r.account_id AND a.organization_id = r.organization_id
WHERE r.organization_id = $1 AND (
 (NOT p.archived AND NOT EXISTS (
    SELECT 1 FROM hosted_account_pool_revision later WHERE later.pool_id = r.pool_id AND later.number > r.number
 )) OR EXISTS (
    SELECT 1 FROM hashboard_pool_intent intent WHERE intent.organization_id = r.organization_id AND intent.pool_revision_id = r.revision_id
 ) OR EXISTS (
    SELECT 1 FROM hosted_account_pool_profile profile WHERE profile.organization_id = r.organization_id AND profile.revision_id = r.revision_id
 ))
ORDER BY r.revision_id;
