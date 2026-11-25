-- name: BindHostedAccountKey :exec
INSERT INTO hosted_account_key (api_key_id, organization_id, account_id, capabilities, include_future_descendants)
VALUES ($1,$2,$3,$4,$5);

-- name: AddHostedAccountKeyDescendant :exec
INSERT INTO hosted_account_key_descendant (organization_id,api_key_id,account_id) VALUES ($1,$2,$3);

-- name: GetHostedAccountKeyAuthority :one
SELECT h.*, a.account_id AS account_uuid,
 ARRAY(SELECT d.account_id FROM hosted_account_key_descendant d WHERE d.organization_id=h.organization_id AND d.api_key_id=h.api_key_id)::bigint[] AS descendant_ids
FROM hosted_account_key h
JOIN api_key k ON k.id=h.api_key_id AND k.organization_id=h.organization_id
JOIN hosted_account a ON a.id=h.account_id AND a.organization_id=h.organization_id
WHERE h.api_key_id=$1 AND h.organization_id=$2 AND k.user_id=$3
 AND k.subject_kind='account' AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at>statement_timestamp());

-- name: ListHostedAccountKeys :many
-- Bound key rows before expanding their retained descendant scopes.
WITH page AS MATERIALIZED (
 SELECT k.id AS api_key_id, k.key_id, k.name, k.prefix, k.created_at, k.expires_at, k.last_used_at,
  k.user_id, h.organization_id, h.capabilities, h.include_future_descendants
 FROM hosted_account_key h JOIN api_key k ON k.id=h.api_key_id AND k.organization_id=h.organization_id
 WHERE h.organization_id=sqlc.arg(organization_id) AND h.account_id=sqlc.arg(account_id)
  AND k.subject_kind='account' AND k.revoked_at IS NULL
  AND (k.user_id=sqlc.arg(user_id) OR sqlc.arg(include_all)::boolean)
  AND (sqlc.narg(cursor_time)::timestamptz IS NULL
   OR k.created_at < sqlc.narg(cursor_time)::timestamptz
   OR (k.created_at = sqlc.narg(cursor_time)::timestamptz AND k.key_id > sqlc.arg(cursor_id)::varchar))
 ORDER BY k.created_at DESC,k.key_id ASC
 LIMIT sqlc.arg(page_limit)::int
)
SELECT page.key_id, page.name, page.prefix, page.created_at, page.expires_at, page.last_used_at,
 u.user_id AS issuer_user_id, u.username AS issuer_username, page.capabilities, page.include_future_descendants,
 ARRAY(SELECT a.account_id FROM hosted_account_key_descendant d JOIN hosted_account a ON a.id=d.account_id AND a.organization_id=d.organization_id
       WHERE d.organization_id=page.organization_id AND d.api_key_id=page.api_key_id ORDER BY a.account_id)::text[] AS descendant_account_ids
FROM page JOIN "user" u ON u.id=page.user_id
ORDER BY page.created_at DESC,page.key_id ASC;

-- name: GetHostedAccountKeyOwner :one
SELECT k.user_id FROM api_key k JOIN hosted_account_key h ON h.api_key_id=k.id AND h.organization_id=k.organization_id
WHERE k.key_id=$1 AND h.organization_id=$2 AND h.account_id=$3 AND k.subject_kind='account' AND k.revoked_at IS NULL;

-- name: RevokeHostedAccountKey :execrows
UPDATE api_key k SET revoked_at=CURRENT_TIMESTAMP FROM hosted_account_key h
WHERE h.api_key_id=k.id AND h.organization_id=k.organization_id
 AND k.key_id=$1 AND h.organization_id=$2 AND h.account_id=$3 AND k.subject_kind='account' AND k.revoked_at IS NULL;
