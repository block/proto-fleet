-- Multi-assignment join queries. A user can hold multiple (role,
-- scope_type, scope_id) rows in the same organization; the per-request
-- permission resolver loads every active row for a (user, org) pair
-- on each authenticated request.

-- name: AssignRole :one
-- Insert a single assignment. Caller is responsible for the
-- privilege-parity check (a caller can only assign a role whose
-- permissions are a subset of the caller's own) before this fires.
-- The partial unique indexes uq_user_org_role_org_scope and
-- uq_user_org_role_site_scope catch re-assignment of the same live
-- (user, role, scope) tuple and surface as AlreadyExists.
-- Soft-deleted rows are excluded from the indexes, so re-assigning
-- after UnassignRole is allowed.
INSERT INTO user_organization_role (
    user_id,
    organization_id,
    role_id,
    scope_type,
    scope_id
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UnassignRole :exec
-- Soft delete so audit trails survive. The handler runs the
-- last-org-scope-SUPER_ADMIN guard (CountOrgScopeSuperAdminsExcludingAssignment)
-- before calling this so an org can never lose its last SUPER_ADMIN.
UPDATE user_organization_role
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetAssignmentByID :one
SELECT *
FROM user_organization_role
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetOrgScopeAssignmentForUser :one
-- Returns the user's live org-scope assignment_id, role_id, and the role's
-- builtin_key in one round-trip. Takes FOR UPDATE on both the assignment
-- row and the user row so concurrent UpdateUserRole / DeactivateUser
-- calls on the same target serialize correctly:
--
--   - Two UpdateUserRole calls on the same user contend on uor; the
--     second sees the first's swap and either sees a different current
--     role or, if the prior swap soft-deleted the assignment, gets
--     ErrNoRows.
--   - UpdateUserRole vs DeactivateUser contend on the user row. Without
--     the user lock, a DeactivateUser that commits between the caller's
--     pre-tx GetUserByExternalID and this read would leave the assignment
--     row live (DeactivateUser only touches "user", not uor), so the
--     swap would proceed against a soft-deleted user and leave stale
--     authz state. Joining "user" with deleted_at IS NULL and locking
--     it forces the swap to surface ErrNoRows once the deactivation
--     commits.
SELECT
    uor.id           AS assignment_id,
    uor.role_id      AS role_id,
    r.builtin_key    AS role_builtin_key
FROM user_organization_role uor
JOIN role r   ON r.id = uor.role_id
             AND r.organization_id = uor.organization_id
JOIN "user" u ON u.id = uor.user_id
WHERE uor.user_id = $1
  AND uor.organization_id = $2
  AND uor.scope_type = 'org'
  AND uor.deleted_at IS NULL
  AND r.deleted_at IS NULL
  AND u.deleted_at IS NULL
FOR UPDATE OF uor, u;

-- name: ListAssignmentsForUser :many
-- Returns every active assignment for a (user, org). The per-request
-- permission resolver joins this against role_permission to produce
-- the effective permission set.
SELECT *
FROM user_organization_role
WHERE user_id = $1
  AND organization_id = $2
  AND deleted_at IS NULL
ORDER BY scope_type, scope_id NULLS FIRST, role_id;

-- name: ListAssignmentsForRole :many
-- The role-delete handler uses this to refuse deletion while
-- assignments still reference the role; the response also lists the
-- offending assignments so the admin can unassign them first.
SELECT *
FROM user_organization_role
WHERE role_id = $1
  AND deleted_at IS NULL
ORDER BY user_id, organization_id;

-- name: CountActiveAssignmentsForRole :one
-- Counts live (user_organization_role, user) pairs. Filtering on
-- u.deleted_at IS NULL matches the resolver and the last-SUPER_ADMIN
-- guards: a role only assigned to deactivated users is not actually
-- granting anything, so DeleteCustomRole should be allowed to clear
-- it rather than block the admin on phantom assignments.
SELECT COUNT(*)::BIGINT AS assignment_count
FROM user_organization_role uor
JOIN "user" u ON u.id = uor.user_id
WHERE uor.role_id = $1
  AND uor.deleted_at IS NULL
  AND u.deleted_at IS NULL;

-- name: ListEffectivePermissionsForUserForUpdate :many
-- Race-safety variant of ListEffectivePermissionsForUser. Same join
-- shape, same row order, same narrowing semantics — but takes
-- FOR UPDATE on every row whose mutation can revoke the caller's
-- effective permissions: the assignment row (uor), the caller's user
-- row (u), and the caller's role row (r). Concurrent:
--
--   - UnassignRole / DeleteCustomRole (target = caller's role)
--       blocks on uor / r
--   - DeactivateUser (caller)
--       blocks on u
--   - UpdateCustomRole (target = caller's role, edits role_permission)
--       blocks on r — getRoleInOrg's read inside the mutation tx then
--       sees our lock and waits, so the role_permission delete/insert
--       can't interleave between our recheck and our commit
--
-- The LEFT JOIN sides (role_permission, permission) cannot participate
-- in FOR UPDATE because they may have no matching row for a
-- zero-permission assignment. We accept that role_permission edits
-- via paths other than UpdateCustomRole (none exist today) would race
-- this check; the practical lock graph through the existing surfaces
-- is closed.
--
-- The non-locking sibling's LEFT JOIN narrowing rule still applies.
SELECT
    uor.id          AS assignment_id,
    uor.role_id     AS role_id,
    uor.scope_type  AS scope_type,
    uor.scope_id    AS scope_id,
    p.key           AS permission_key
FROM user_organization_role uor
JOIN role r              ON r.id = uor.role_id
                        AND r.organization_id = uor.organization_id
JOIN "user" u            ON u.id = uor.user_id
LEFT JOIN role_permission rp ON rp.role_id = r.id
LEFT JOIN permission p       ON p.id = rp.permission_id
WHERE uor.user_id = $1
  AND uor.organization_id = $2
  AND uor.deleted_at IS NULL
  AND r.deleted_at IS NULL
  AND u.deleted_at IS NULL
ORDER BY uor.id, p.key NULLS FIRST
FOR UPDATE OF uor, u, r;

-- name: ListEffectivePermissionsForUser :many
-- Single-query resolver source: one row per (assignment, permission)
-- pair the user holds within an organization, with a NULL permission
-- column when the assignment's role has no permissions attached.
--
-- The LEFT JOIN on role_permission and permission is load-bearing for
-- the narrowing semantics: a site-scope assignment whose role grants
-- zero permissions must still surface in the resolver as a "site has
-- an assignment" marker. Without it, the resolver would not record
-- bySite[siteID] for that site, narrowing would collapse, and the
-- caller's org-scope grant would silently apply at the site the
-- empty role was meant to lock down.
--
-- The JOIN on "user" with u.deleted_at IS NULL is the revocation
-- backstop: a deactivated user's next request reads an empty
-- EffectivePermissions and denies everything, and a mutation-
-- transaction recheck via LoadEffectiveTx refuses to commit grants
-- against a caller whose row has been soft-deleted mid-tx. Same
-- liveness rule the last-SUPER_ADMIN guards below already enforce.
--
-- The resolver walks this slice to evaluate Has(key, ResourceContext)
-- with the plan's narrowing rule: site-scope assignment overrides the
-- org grant at that site; site-scope absence falls back to org-scope.
SELECT
    uor.id          AS assignment_id,
    uor.role_id     AS role_id,
    uor.scope_type  AS scope_type,
    uor.scope_id    AS scope_id,
    p.key           AS permission_key
FROM user_organization_role uor
JOIN role r              ON r.id = uor.role_id
                        AND r.organization_id = uor.organization_id
JOIN "user" u            ON u.id = uor.user_id
LEFT JOIN role_permission rp ON rp.role_id = r.id
LEFT JOIN permission p       ON p.id = rp.permission_id
WHERE uor.user_id = $1
  AND uor.organization_id = $2
  AND uor.deleted_at IS NULL
  AND r.deleted_at IS NULL
  AND u.deleted_at IS NULL
ORDER BY uor.id, p.key NULLS FIRST;

-- name: LockAndCountOrgScopeSuperAdmins :one
-- Last-SUPER_ADMIN guard. Locks every live org-scope SUPER_ADMIN
-- assignment in the org and returns the count. Callers that intend to
-- demote/unassign/deactivate a SUPER_ADMIN compare against 1: if the
-- count is <= 1 they hold the last seat and must refuse.
--
-- The FOR UPDATE OF uor inside the CTE makes two concurrent demotions
-- contend on the same SA row set, not on their respective "other" rows
-- — without this, both txs lock different "remaining" rows, both see
-- count=1, and both commit. Aggregates can't appear in a FOR UPDATE
-- query directly, so the lock-and-count pattern uses a CTE to take the
-- lock on the matching rows and then aggregate over the result.
--
-- "Live" means: the assignment row, its role row, AND the underlying
-- user are all non-deleted. Without the user join, a deactivated
-- SUPER_ADMIN user would still preserve the count (their assignment
-- row survives soft-delete-of-user), and the guard would let a caller
-- remove the last actually-usable SUPER_ADMIN.
WITH locked_super_admins AS (
    SELECT uor.id
    FROM user_organization_role uor
    JOIN role r   ON r.id = uor.role_id
                 AND r.organization_id = uor.organization_id
    JOIN "user" u ON u.id = uor.user_id
    WHERE uor.organization_id = $1
      AND uor.scope_type = 'org'
      AND uor.deleted_at IS NULL
      AND r.deleted_at IS NULL
      AND u.deleted_at IS NULL
      AND r.builtin_key = 'SUPER_ADMIN'
    FOR UPDATE OF uor
)
SELECT COUNT(*)::BIGINT AS super_admin_count FROM locked_super_admins;

-- name: CountOrgScopeSuperAdminsExcludingUser :one
-- Same guard, but for DeactivateUser: counts live SUPER_ADMINs in
-- the org excluding any assignment held by the user being
-- deactivated. Same liveness filters as above so a deactivated user
-- never inflates the count.
SELECT COUNT(*)::BIGINT AS super_admin_count
FROM user_organization_role uor
JOIN role r   ON r.id = uor.role_id
             AND r.organization_id = uor.organization_id
JOIN "user" u ON u.id = uor.user_id
WHERE uor.organization_id = $1
  AND uor.scope_type = 'org'
  AND uor.deleted_at IS NULL
  AND r.deleted_at IS NULL
  AND u.deleted_at IS NULL
  AND r.builtin_key = 'SUPER_ADMIN'
  AND uor.user_id != $2;
