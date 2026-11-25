-- name: CreateHostedAccountPool :one
INSERT INTO hosted_account_pool (pool_id, organization_id, account_id)
VALUES ($1,$2,$3) RETURNING *;

-- name: CreateHostedAccountPoolRevision :one
INSERT INTO hosted_account_pool_revision (revision_id,organization_id,account_id,pool_id,number,name,slots)
VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING *;

-- name: GetHostedAccountPool :one
SELECT * FROM hosted_account_pool
WHERE organization_id=$1 AND account_id=$2 AND pool_id=$3;

-- name: GetHostedAccountPoolRevision :one
SELECT * FROM hosted_account_pool_revision
WHERE organization_id=sqlc.arg(organization_id) AND account_id=sqlc.arg(account_id)
 AND pool_id=sqlc.arg(pool_id)
 AND (sqlc.arg(revision_id)::text='' OR revision_id=sqlc.arg(revision_id)::text)
ORDER BY number DESC LIMIT 1;

-- name: GetHostedAccountPoolRevisionByID :one
SELECT * FROM hosted_account_pool_revision
WHERE organization_id=$1 AND account_id=$2 AND revision_id=$3;

-- name: ListHostedAccountPools :many
SELECT r.*, p.archived FROM hosted_account_pool p
JOIN LATERAL (
 SELECT * FROM hosted_account_pool_revision r
 WHERE r.organization_id=p.organization_id AND r.account_id=p.account_id AND r.pool_id=p.pool_id
 ORDER BY r.number DESC LIMIT 1
) r ON TRUE
WHERE p.organization_id=sqlc.arg(organization_id) AND p.account_id=sqlc.arg(account_id)
 AND (sqlc.arg(include_archived)::boolean OR NOT p.archived)
 AND p.pool_id>sqlc.arg(after_pool_id)::text
ORDER BY p.pool_id LIMIT sqlc.arg(page_limit);

-- name: ArchiveHostedAccountPool :execrows
UPDATE hosted_account_pool p SET archived=TRUE
WHERE p.organization_id=$1 AND p.account_id=$2 AND p.pool_id=$3
 AND NOT EXISTS (
 SELECT 1 FROM hosted_account_pool_profile f
 JOIN hosted_account_pool_revision r ON r.revision_id=f.revision_id
  AND r.organization_id=f.organization_id AND r.account_id=f.account_id
 WHERE f.organization_id=p.organization_id AND f.account_id=p.account_id AND r.pool_id=p.pool_id
 );

-- name: GetHostedAccountPoolProfile :one
SELECT f.*, r.pool_id FROM hosted_account_pool_profile f
LEFT JOIN hosted_account_pool_revision r ON r.revision_id=f.revision_id
 AND r.organization_id=f.organization_id AND r.account_id=f.account_id
WHERE f.organization_id=$1 AND f.account_id=$2;

-- name: SetHostedAccountPoolProfile :one
INSERT INTO hosted_account_pool_profile (organization_id,account_id,revision_id)
VALUES ($1,$2,$3)
ON CONFLICT (organization_id,account_id) DO UPDATE
SET revision_id=EXCLUDED.revision_id,revision=hosted_account_pool_profile.revision+1,updated_at=CURRENT_TIMESTAMP
RETURNING revision;
