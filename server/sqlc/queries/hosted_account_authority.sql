-- name: ResolveHostedAccountAuthority :one
-- Resolve direct and explicitly granted ancestor authority in one snapshot.
WITH RECURSIVE path AS (
 SELECT a.* FROM hosted_account a WHERE a.organization_id=sqlc.arg(organization_id) AND a.account_id=sqlc.arg(account_id)
 UNION ALL
 SELECT p.* FROM hosted_account p JOIN path c ON p.id=c.parent_id AND p.organization_id=c.organization_id
), target AS (
 SELECT a.* FROM path a ORDER BY depth DESC LIMIT 1
), policy AS (
 SELECT bit_and(capabilities)::bigint AS ceiling, bool_or(suspended) AS suspended,
        string_agg(id::text || ':' || authority_revision::text, ',' ORDER BY depth) AS state
 FROM path
), sources AS (
 SELECT CASE m.role WHEN 'viewer' THEN 1 WHEN 'pool_operator' THEN 7 WHEN 'account_administrator' THEN 31 ELSE 0 END::bigint AS capabilities,
        m.role, 'member:' || m.revision::text AS witness
 FROM hosted_account_membership m JOIN target t ON t.id=m.account_id AND t.organization_id=m.organization_id
 WHERE m.user_id=sqlc.arg(user_id) AND m.revoked_at IS NULL
 UNION ALL
 SELECT (g.capabilities & CASE WHEN ancestor.depth=0 THEN 31 ELSE
        CASE member.role WHEN 'viewer' THEN 1 WHEN 'pool_operator' THEN 7 WHEN 'account_administrator' THEN 31 ELSE 0 END END)::bigint,
        ''::text, 'grant:' || g.ancestor_id::text || ':' || g.revision::text || ':' ||
        CASE WHEN ancestor.depth=0 THEN COALESCE((
          SELECT string_agg(ur.id::text || ':' || ur.updated_at::text || ':' || r.updated_at::text, ',' ORDER BY ur.id)
          FROM user_organization_role ur JOIN role r ON r.id=ur.role_id AND r.organization_id=ur.organization_id AND r.deleted_at IS NULL
          JOIN role_permission rp ON rp.role_id=r.id JOIN permission p ON p.id=rp.permission_id AND p.key='account:manage'
          WHERE ur.organization_id=g.organization_id AND ur.user_id=sqlc.arg(user_id) AND ur.deleted_at IS NULL AND ur.scope_type='org'
        ),'') ELSE member.revision::text END
 FROM hosted_account_ancestor_grant g JOIN target t ON t.id=g.account_id AND t.organization_id=g.organization_id
 JOIN path ancestor ON ancestor.id=g.ancestor_id AND ancestor.depth<t.depth
 LEFT JOIN hosted_account_membership member ON member.organization_id=g.organization_id AND member.account_id=g.ancestor_id
     AND member.user_id=sqlc.arg(user_id) AND member.revoked_at IS NULL
 WHERE g.revoked_at IS NULL AND ((ancestor.depth>0 AND member.user_id IS NOT NULL) OR (ancestor.depth=0 AND EXISTS (
   SELECT 1 FROM user_organization_role ur JOIN role r ON r.id=ur.role_id AND r.organization_id=ur.organization_id AND r.deleted_at IS NULL
   JOIN role_permission rp ON rp.role_id=r.id JOIN permission p ON p.id=rp.permission_id AND p.key='account:manage'
   WHERE ur.organization_id=g.organization_id AND ur.user_id=sqlc.arg(user_id) AND ur.deleted_at IS NULL AND ur.scope_type='org'
 )))
)
SELECT t.account_id, COALESCE(MAX(NULLIF(s.role,'')),'')::text AS role,
       COALESCE(bit_or(s.capabilities),0)::bigint AS source_capabilities,
       (COALESCE(bit_or(s.capabilities),0) & policy.ceiling)::bigint AS capabilities,
       (policy.state || '/' || COALESCE(string_agg(s.witness, ';' ORDER BY s.witness),''))::text AS authority_state
FROM target t CROSS JOIN policy
JOIN organization o ON o.id=t.organization_id AND o.deleted_at IS NULL
JOIN user_organization uo ON uo.organization_id=t.organization_id AND uo.user_id=sqlc.arg(user_id) AND uo.deleted_at IS NULL
JOIN "user" u ON u.id=uo.user_id AND u.deleted_at IS NULL
LEFT JOIN sources s ON TRUE
WHERE t.depth>0 AND NOT policy.suspended
GROUP BY t.account_id,policy.ceiling,policy.state;

-- name: RenameHostedAccount :exec
UPDATE hosted_account SET name=$1, revision=revision+1, updated_at=CURRENT_TIMESTAMP
WHERE organization_id=$2 AND id=$3;

-- name: ListAuthorizedHostedAccounts :many
SELECT a.* FROM hosted_account a
JOIN hosted_account root ON root.organization_id=a.organization_id AND root.depth=0
LEFT JOIN hosted_account parent ON parent.id=a.parent_id AND parent.organization_id=a.organization_id
JOIN organization o ON o.id=a.organization_id AND o.deleted_at IS NULL
JOIN user_organization uo ON uo.organization_id=a.organization_id AND uo.user_id=sqlc.arg(user_id) AND uo.deleted_at IS NULL
JOIN "user" u ON u.id=uo.user_id AND u.deleted_at IS NULL
WHERE a.organization_id=sqlc.arg(organization_id) AND NOT a.suspended AND NOT root.suspended AND NOT COALESCE(parent.suspended,FALSE)
AND (a.capabilities & COALESCE(parent.capabilities,31) & root.capabilities & 1)<>0
AND ((a.depth=0 AND EXISTS (
 SELECT 1 FROM user_organization_role ur JOIN role r ON r.id=ur.role_id AND r.organization_id=ur.organization_id AND r.deleted_at IS NULL
 JOIN role_permission rp ON rp.role_id=r.id JOIN permission p ON p.id=rp.permission_id AND p.key='account:manage'
 WHERE ur.user_id=u.id AND ur.organization_id=a.organization_id AND ur.deleted_at IS NULL AND ur.scope_type='org'
)) OR (a.depth>0 AND (EXISTS (
 SELECT 1 FROM hosted_account_membership m WHERE m.account_id=a.id AND m.organization_id=a.organization_id AND m.user_id=u.id AND m.revoked_at IS NULL
) OR EXISTS (
 SELECT 1 FROM hosted_account_ancestor_grant g
 JOIN hosted_account ancestor ON ancestor.id=g.ancestor_id AND ancestor.organization_id=g.organization_id
 WHERE g.organization_id=a.organization_id AND g.account_id=a.id AND g.revoked_at IS NULL AND (g.capabilities & 1)<>0
 AND ((ancestor.depth>0 AND EXISTS (
  SELECT 1 FROM hosted_account_membership m WHERE m.account_id=ancestor.id AND m.organization_id=a.organization_id AND m.user_id=u.id AND m.revoked_at IS NULL
 )) OR (ancestor.depth=0 AND EXISTS (
  SELECT 1 FROM user_organization_role ur JOIN role r ON r.id=ur.role_id AND r.organization_id=ur.organization_id AND r.deleted_at IS NULL
  JOIN role_permission rp ON rp.role_id=r.id JOIN permission p ON p.id=rp.permission_id AND p.key='account:manage'
  WHERE ur.user_id=u.id AND ur.organization_id=a.organization_id AND ur.deleted_at IS NULL AND ur.scope_type='org'
 )))
)))) ORDER BY a.depth,a.name,a.account_id;

-- name: GetHostedAccountAncestorGrant :one
SELECT * FROM hosted_account_ancestor_grant WHERE organization_id=$1 AND account_id=$2 AND ancestor_id=$3 AND revoked_at IS NULL;

-- name: FindHostedAccountMemberByUUID :one
-- Membership administration may look up only members of the selected account.
SELECT u.id FROM "user" u JOIN hosted_account_membership m ON m.user_id=u.id AND m.revoked_at IS NULL
JOIN user_organization uo ON uo.user_id=u.id AND uo.organization_id=m.organization_id AND uo.deleted_at IS NULL
WHERE m.organization_id=$1 AND m.account_id=$2 AND u.user_id=$3 AND u.deleted_at IS NULL;

-- name: HasHostedAccountHostAuthority :one
SELECT EXISTS (
 SELECT 1 FROM user_organization_role ur
 JOIN "user" u ON u.id=ur.user_id AND u.deleted_at IS NULL
 JOIN user_organization uo ON uo.user_id=u.id AND uo.organization_id=ur.organization_id AND uo.deleted_at IS NULL
 JOIN organization o ON o.id=ur.organization_id AND o.deleted_at IS NULL
 JOIN role r ON r.id=ur.role_id AND r.organization_id=ur.organization_id AND r.deleted_at IS NULL
 JOIN role_permission rp ON rp.role_id=r.id JOIN permission p ON p.id=rp.permission_id AND p.key='account:manage'
 WHERE ur.user_id=$1 AND ur.organization_id=$2 AND ur.deleted_at IS NULL AND ur.scope_type='org'
)::boolean AS authorized;
