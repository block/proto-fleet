-- RBAC v2 foundation: permission catalog, role→permission join, and
-- multi-assignment user→role rows with org/site scope.
--
-- The existing user_organization.role_id column is preserved unchanged
-- here; migration 000053 backfills assignments into the new
-- user_organization_role table. A later migration will neutralize the
-- legacy column with a raising trigger on non-NULL writes (alongside
-- the caller swap that retires it), and a final migration drops it
-- once a soak window confirms no callers remain.

-- Per-org role model. Every role (built-in or custom) is owned by an
-- organization so editing it cannot leak across tenants. The legacy
-- global ADMIN row from migration 000002 (and any SUPER_ADMIN row
-- created by onboarding before this migration) is left with
-- organization_id NULL temporarily — migration 000052 repoints
-- existing user_organization references to per-org replacements and
-- then soft-deletes the legacy rows.
ALTER TABLE role
    ADD COLUMN is_builtin       BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN builtin_key      VARCHAR(64) NULL,
    ADD COLUMN organization_id  BIGINT NULL;

ALTER TABLE role
    ADD CONSTRAINT fk_role_organization FOREIGN KEY (organization_id)
        REFERENCES organization(id) ON DELETE CASCADE;

-- Global name uniqueness from migration 000002 is no longer correct
-- under per-org roles — each org should be able to name its own
-- "FieldTech Plus" custom role independently. Replace with partial
-- unique indexes scoped to live rows.
ALTER TABLE role DROP CONSTRAINT uq_role_name;

CREATE UNIQUE INDEX uq_role_org_builtin_key
    ON role(organization_id, builtin_key)
    WHERE is_builtin = TRUE AND deleted_at IS NULL;

CREATE UNIQUE INDEX uq_role_org_custom_name
    ON role(organization_id, name)
    WHERE is_builtin = FALSE AND deleted_at IS NULL;

-- An is_builtin row must always carry a builtin_key; an is_builtin=FALSE
-- row must not. Enforced at the DB so application bugs cannot create
-- a built-in row with no key or a custom row pretending to be a builtin.
ALTER TABLE role
    ADD CONSTRAINT chk_role_builtin_key_matches_flag CHECK (
        (is_builtin = TRUE  AND builtin_key IS NOT NULL) OR
        (is_builtin = FALSE AND builtin_key IS NULL)
    );

-- Composite-key target so child tables (user_organization_role) can FK on
-- (role_id, organization_id) and reject a cross-tenant pointer at the DB
-- layer. Without this, an assignment could bind a user in org A to a role
-- owned by org B — a cross-tenant authorization escalation path. The
-- legacy global rows from migration 000002 (still organization_id NULL
-- here) are excluded; they're soft-deleted by migration 000052 and the
-- backfill never produces an assignment that points at them.
ALTER TABLE role
    ADD CONSTRAINT uq_role_id_org_id UNIQUE (id, organization_id);

-- Catalog of permission keys. Source of truth is
-- server/internal/domain/authz/catalog.go; this table is reconciled at
-- startup so a fresh install and an upgrade converge to the same state.
CREATE TABLE permission (
    id          BIGSERIAL    PRIMARY KEY,
    key         VARCHAR(128) NOT NULL,
    description TEXT         NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq_permission_key UNIQUE (key)
);

-- Role↔permission join. ON DELETE RESTRICT on permission_id so a
-- permission cannot be silently dropped while still referenced;
-- obsolete-permission cleanup is a deliberate manual migration step.
CREATE TABLE role_permission (
    role_id       BIGINT NOT NULL REFERENCES role(id) ON DELETE CASCADE,
    permission_id BIGINT NOT NULL REFERENCES permission(id) ON DELETE RESTRICT,

    PRIMARY KEY (role_id, permission_id)
);

-- Multi-assignment join: a user can hold multiple (role, scope) pairs
-- in the same organization. scope_type is 'org' (scope_id IS NULL) or
-- 'site' (scope_id references site.id within the same organization).
-- Building scope is deferred to a follow-up plan; when it ships, the
-- CHECK is relaxed and a second composite FK is added.
CREATE TABLE user_organization_role (
    id              BIGSERIAL   PRIMARY KEY,
    user_id         BIGINT      NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    organization_id BIGINT      NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    role_id         BIGINT      NOT NULL,
    scope_type      VARCHAR(16) NOT NULL,
    scope_id        BIGINT      NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at      TIMESTAMPTZ NULL,

    CONSTRAINT chk_user_org_role_scope_type
        CHECK (scope_type IN ('org', 'site')),

    -- scope_id is NULL for org-scope, NOT NULL for site-scope. Mismatched
    -- combinations are rejected at the DB layer so application bugs cannot
    -- write "org-scope but pointing at site 42" or vice versa.
    CONSTRAINT chk_user_org_role_scope_id_matches_type CHECK (
        (scope_type = 'org'  AND scope_id IS NULL) OR
        (scope_type = 'site' AND scope_id IS NOT NULL)
    ),

    -- Composite FK on role so an assignment row can only point at a role
    -- owned by the same organization. Without this, a buggy or
    -- malicious INSERT could bind a user in org A to a role owned by
    -- org B and the resolver would grant org A's user that role's
    -- permissions. DB-enforced tenant isolation closes the path
    -- structurally; the application-layer filter in
    -- ListEffectivePermissionsForUser is belt-and-suspenders.
    CONSTRAINT fk_user_org_role_role FOREIGN KEY (role_id, organization_id)
        REFERENCES role(id, organization_id) ON DELETE RESTRICT,

    -- Composite FK uses the (id, org_id) unique key on `site` shipped by
    -- multi-site Phase 1 (migration 000043). Pins a site-scoped
    -- assignment to a site that belongs to the same organization. The
    -- FK is DEFERRABLE INITIALLY DEFERRED so a transactional
    -- re-assignment that deletes and re-inserts within one tx is
    -- evaluated at commit.
    CONSTRAINT fk_user_org_role_site FOREIGN KEY (scope_id, organization_id)
        REFERENCES site(id, org_id) ON DELETE CASCADE
        DEFERRABLE INITIALLY DEFERRED
);

-- Hot path: the resolver loads every active assignment for a (user, org)
-- pair on every authenticated request. Partial index on deleted_at IS NULL
-- so the index stays small as soft-deletes accumulate.
CREATE INDEX idx_user_organization_role_user_org
    ON user_organization_role(user_id, organization_id)
    WHERE deleted_at IS NULL;

-- Idempotency / uniqueness.
--
-- A naive `UNIQUE (user_id, organization_id, role_id, scope_type,
-- scope_id)` constraint does NOT enforce uniqueness for org-scope rows
-- because Postgres treats NULL values as distinct in unique
-- constraints — and scope_id is always NULL for scope_type='org'. The
-- same constraint also blocks re-creation of a site-scope assignment
-- after a soft-delete, because the deleted-but-still-present row
-- collides with the new one.
--
-- Two partial unique indexes solve both issues: each only covers
-- live (deleted_at IS NULL) rows, and the org variant omits scope_id
-- entirely so the NULL-distinct trap can't bite.
CREATE UNIQUE INDEX uq_user_org_role_org_scope
    ON user_organization_role(user_id, organization_id, role_id)
    WHERE scope_type = 'org' AND deleted_at IS NULL;

CREATE UNIQUE INDEX uq_user_org_role_site_scope
    ON user_organization_role(user_id, organization_id, role_id, scope_id)
    WHERE scope_type = 'site' AND deleted_at IS NULL;

CREATE TRIGGER update_user_organization_role_updated_at
    BEFORE UPDATE ON user_organization_role
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
