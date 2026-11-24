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
 SELECT CASE m.role WHEN 'viewer' THEN 1 WHEN 'pool_operator' THEN 7 WHEN 'account_administrator' THEN 63 ELSE 0 END::bigint AS capabilities,
        m.role, 'member:' || m.revision::text AS witness
 FROM hosted_account_membership m JOIN target t ON t.id=m.account_id AND t.organization_id=m.organization_id
 WHERE m.user_id=sqlc.arg(user_id) AND m.revoked_at IS NULL
 UNION ALL
 SELECT (g.capabilities & CASE WHEN ancestor.depth=0 THEN 63 ELSE
        CASE member.role WHEN 'viewer' THEN 1 WHEN 'pool_operator' THEN 7 WHEN 'account_administrator' THEN 63 ELSE 0 END END)::bigint,
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

-- name: ListHostedAccountSummaries :many
-- Resolve the entire account list in one snapshot. A parent filter is used only
-- after delegation authorization and includes policy-only children, with no
-- private role or capabilities unless explicit live authority grants viewing.
WITH actor AS (
 SELECT u.id FROM "user" u
 JOIN user_organization uo ON uo.user_id=u.id AND uo.organization_id=sqlc.arg(organization_id) AND uo.deleted_at IS NULL
 JOIN organization o ON o.id=uo.organization_id AND o.deleted_at IS NULL
 WHERE u.id=sqlc.arg(user_id) AND u.deleted_at IS NULL
), host_authority AS (
 SELECT EXISTS (
  SELECT 1 FROM user_organization_role ur
  JOIN role r ON r.id=ur.role_id AND r.organization_id=ur.organization_id AND r.deleted_at IS NULL
  JOIN role_permission rp ON rp.role_id=r.id
  JOIN permission p ON p.id=rp.permission_id AND p.key='account:manage'
  JOIN actor ON actor.id=ur.user_id
  WHERE ur.organization_id=sqlc.arg(organization_id) AND ur.deleted_at IS NULL AND ur.scope_type='org'
 ) AS authorized
), memberships AS (
 SELECT m.account_id, m.role,
        CASE m.role WHEN 'viewer' THEN 1 WHEN 'pool_operator' THEN 7 WHEN 'account_administrator' THEN 63 ELSE 0 END::bigint AS capabilities
 FROM hosted_account_membership m JOIN actor ON actor.id=m.user_id
 WHERE m.organization_id=sqlc.arg(organization_id) AND m.revoked_at IS NULL
), accounts AS (
 SELECT a.*, COALESCE(parent.account_id,'')::text AS parent_account_id,
        (a.capabilities & COALESCE(parent.capabilities,63) & root.capabilities)::bigint AS ceiling,
        (a.suspended OR root.suspended OR COALESCE(parent.suspended,FALSE)) AS path_suspended,
        root.id AS root_id,
        (a.ticketing_enabled AND root.ticketing_enabled AND COALESCE(parent.ticketing_enabled,TRUE)) AS effective_ticketing_enabled
 FROM hosted_account a
 JOIN hosted_account root ON root.organization_id=a.organization_id AND root.depth=0
 LEFT JOIN hosted_account parent ON parent.id=a.parent_id AND parent.organization_id=a.organization_id
 WHERE a.organization_id=sqlc.arg(organization_id) AND EXISTS (SELECT 1 FROM actor)
 AND (sqlc.narg(parent_id)::bigint IS NULL OR a.parent_id=sqlc.narg(parent_id)::bigint)
), sources AS (
 SELECT a.id, m.role, m.capabilities
 FROM accounts a JOIN memberships m ON m.account_id=a.id WHERE a.depth>0
 UNION ALL
 SELECT a.id, ''::text, (g.capabilities & CASE WHEN ancestor.depth=0 THEN 63 ELSE m.capabilities END)::bigint
 FROM accounts a
 JOIN hosted_account_ancestor_grant g ON g.account_id=a.id AND g.organization_id=a.organization_id AND g.revoked_at IS NULL
 JOIN hosted_account ancestor ON ancestor.id=g.ancestor_id AND ancestor.organization_id=g.organization_id AND ancestor.depth<a.depth
 LEFT JOIN memberships m ON m.account_id=ancestor.id
 CROSS JOIN host_authority h
 WHERE (ancestor.id=a.parent_id OR ancestor.id=a.root_id)
 AND ((ancestor.depth=0 AND h.authorized) OR (ancestor.depth>0 AND m.account_id IS NOT NULL))
), authority AS (
 SELECT id, COALESCE(MAX(NULLIF(role,'')),'')::text AS role, bit_or(capabilities)::bigint AS capabilities
 FROM sources GROUP BY id
), summaries AS (
 SELECT a.account_id, a.parent_account_id, a.name, a.depth, a.capabilities, a.suspended, a.revision,
        CASE WHEN a.depth=0 AND h.authorized AND NOT a.path_suspended AND (a.ceiling & 1)<>0 THEN 16
             WHEN a.depth>0 AND NOT a.path_suspended AND (COALESCE(auth.capabilities,0) & a.ceiling & 1)<>0
             THEN COALESCE(auth.capabilities,0) & a.ceiling ELSE 0 END::bigint AS effective_capabilities,
        CASE WHEN a.depth>0 AND NOT a.path_suspended AND (COALESCE(auth.capabilities,0) & a.ceiling & 1)<>0
             THEN COALESCE(auth.role,'') ELSE '' END::text AS role
 FROM accounts a CROSS JOIN host_authority h LEFT JOIN authority auth ON auth.id=a.id
), management_clients AS (
 SELECT id FROM accounts WHERE depth=1 AND parent_id=sqlc.narg(parent_id)::bigint
), assignment_counts AS (
 SELECT a.client_id,
        count(*) FILTER (WHERE a.kind=5)::bigint AS miner_count,
        0::bigint AS module_count,
        count(DISTINCT a.device_id) FILTER (WHERE a.kind=7)::bigint AS container_count,
        count(*) FILTER (WHERE a.kind=7)::bigint AS hashboard_count
 FROM hosted_account_asset a JOIN management_clients c ON c.id=a.client_id
 WHERE a.organization_id=sqlc.arg(organization_id)
 GROUP BY a.client_id
), login_counts AS (
 SELECT m.account_id, count(*)::bigint AS login_count
 FROM hosted_account_membership m JOIN management_clients c ON c.id=m.account_id
 JOIN "user" u ON u.id=m.user_id AND u.deleted_at IS NULL
 JOIN user_organization uo ON uo.user_id=m.user_id AND uo.organization_id=m.organization_id AND uo.deleted_at IS NULL
 WHERE m.organization_id=sqlc.arg(organization_id) AND m.revoked_at IS NULL
 GROUP BY m.account_id
)
SELECT s.*, a.ticketing_enabled,
       (tm.user_id IS NOT NULL AND a.effective_ticketing_enabled AND (s.effective_capabilities & 1)<>0)::boolean AS ticket_access,
       COALESCE(ac.miner_count,0)::bigint AS miner_count,
       COALESCE(ac.module_count,0)::bigint AS module_count,
       COALESCE(ac.hashboard_count,0)::bigint AS hashboard_count,
       COALESCE(ac.container_count,0)::bigint AS container_count,
       COALESCE(lc.login_count,0)::bigint AS login_count
FROM summaries s JOIN accounts a ON a.account_id=s.account_id
LEFT JOIN hosted_account_membership tm ON tm.organization_id=a.organization_id AND tm.account_id=a.id AND tm.user_id=sqlc.arg(user_id) AND tm.revoked_at IS NULL
LEFT JOIN assignment_counts ac ON ac.client_id=a.id
LEFT JOIN login_counts lc ON lc.account_id=a.id
WHERE sqlc.narg(parent_id)::bigint IS NOT NULL OR s.effective_capabilities<>0
ORDER BY s.depth,s.name,s.account_id;

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
