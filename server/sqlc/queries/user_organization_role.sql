-- Multi-assignment join queries. A user can hold multiple (role,
-- scope_type, scope_id) rows in the same organization; the resolver
-- (U6) loads every active row for a (user, org) pair on each request.

-- name: AssignRole :one
-- Insert a single assignment. Caller is responsible for the
-- privilege-parity check (U8) before this fires. The unique constraint
-- uq_user_org_role_scope catches re-assignment of the same
-- (user, role, scope_type, scope_id) and surfaces as AlreadyExists.
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
-- Soft delete so audit trails survive. The last-SUPER_ADMIN check (U8)
-- runs before this fires.
UPDATE user_organization_role
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetAssignmentByID :one
SELECT *
FROM user_organization_role
WHERE id = $1
  AND deleted_at IS NULL;

-- name: ListAssignmentsForUser :many
-- Returns every active assignment for a (user, org). The resolver in
-- U6 joins this against role_permission to produce the effective
-- permission set.
SELECT *
FROM user_organization_role
WHERE user_id = $1
  AND organization_id = $2
  AND deleted_at IS NULL
ORDER BY scope_type, scope_id NULLS FIRST, role_id;

-- name: ListAssignmentsForRole :many
-- Used by DeleteCustomRole to refuse deletion while assignments still
-- reference the role.
SELECT *
FROM user_organization_role
WHERE role_id = $1
  AND deleted_at IS NULL
ORDER BY user_id, organization_id;

-- name: CountActiveAssignmentsForRole :one
SELECT COUNT(*)::BIGINT AS assignment_count
FROM user_organization_role
WHERE role_id = $1
  AND deleted_at IS NULL;

-- name: ListEffectivePermissionsForUser :many
-- Single-query resolver source: every (assignment_id, scope_type,
-- scope_id, permission_key) triple the user holds within an
-- organization. U6 walks this slice to evaluate Has(key,
-- ResourceContext) with the narrowing semantics described in the plan.
SELECT
    uor.id          AS assignment_id,
    uor.role_id     AS role_id,
    uor.scope_type  AS scope_type,
    uor.scope_id    AS scope_id,
    p.key           AS permission_key
FROM user_organization_role uor
JOIN role_permission rp ON rp.role_id = uor.role_id
JOIN permission p       ON p.id = rp.permission_id
JOIN role r             ON r.id = uor.role_id
WHERE uor.user_id = $1
  AND uor.organization_id = $2
  AND uor.deleted_at IS NULL
  AND r.deleted_at IS NULL
ORDER BY uor.id, p.key;

-- name: CountOrgScopeSuperAdminsExcludingAssignment :one
-- Last-SUPER_ADMIN guard (U8). Returns the number of active
-- org-scope SUPER_ADMIN assignments in the org, excluding the given
-- assignment id. UnassignRole and DeactivateUser refuse to proceed
-- when this would drop to zero.
SELECT COUNT(*)::BIGINT AS super_admin_count
FROM user_organization_role uor
JOIN role r ON r.id = uor.role_id
WHERE uor.organization_id = $1
  AND uor.scope_type = 'org'
  AND uor.deleted_at IS NULL
  AND r.builtin_key = 'SUPER_ADMIN'
  AND uor.id != $2;

-- name: CountOrgScopeSuperAdminsExcludingUser :one
-- Same guard, but for DeactivateUser: counts SUPER_ADMINs in the org
-- excluding any assignment held by the user being deactivated.
SELECT COUNT(*)::BIGINT AS super_admin_count
FROM user_organization_role uor
JOIN role r ON r.id = uor.role_id
WHERE uor.organization_id = $1
  AND uor.scope_type = 'org'
  AND uor.deleted_at IS NULL
  AND r.builtin_key = 'SUPER_ADMIN'
  AND uor.user_id != $2;
