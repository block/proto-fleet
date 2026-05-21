-- RBAC v2 foundation: permission catalog, role→permission join, and
-- multi-assignment user→role rows with org/site scope.
--
-- The existing user_organization.role_id column is preserved unchanged
-- here; U5 (migration 000053) backfills assignments and neutralizes the
-- legacy column. U12 will drop it once the soak period confirms no
-- callers remain.

-- Built-in awareness on the existing role table. builtin_key is the
-- stable identifier code uses (SUPER_ADMIN, ADMIN, FIELD_TECH) so seed
-- reordering does not break references.
ALTER TABLE role
    ADD COLUMN is_builtin BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN builtin_key VARCHAR(64) NULL,
    ADD CONSTRAINT uq_role_builtin_key UNIQUE (builtin_key);

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
    role_id         BIGINT      NOT NULL REFERENCES role(id) ON DELETE RESTRICT,
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

    -- One row per (user, role, scope) — the unique key is the structural
    -- guarantee that re-saving the same assignment is idempotent.
    CONSTRAINT uq_user_org_role_scope UNIQUE
        (user_id, organization_id, role_id, scope_type, scope_id),

    -- Composite FK uses the (id, org_id) unique key on `site` shipped by
    -- multi-site Phase 1 (migration 000043). This pins a site-scoped
    -- assignment to a site that belongs to the same organization —
    -- DB-enforced tenant isolation, not application-layer only. The FK is
    -- DEFERRABLE INITIALLY DEFERRED so a transactional re-assignment that
    -- deletes and re-inserts within one tx is evaluated at commit.
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

CREATE TRIGGER update_user_organization_role_updated_at
    BEFORE UPDATE ON user_organization_role
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
