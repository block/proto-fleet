-- Proto Fleet PostgreSQL Core Tables
-- Creates user, role, organization, and user_organization tables

-- =====================================================
-- User table
-- =====================================================
CREATE TABLE "user" (
    id                        BIGSERIAL PRIMARY KEY,
    user_id                   VARCHAR(36) NOT NULL,
    username                  VARCHAR(255) NOT NULL,
    password_hash             TEXT NOT NULL,
    password_updated_at       TIMESTAMPTZ NULL,
    last_login_at             TIMESTAMPTZ NULL,
    requires_password_change  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at                TIMESTAMPTZ NULL,

    CONSTRAINT uq_user_username UNIQUE (username),
    CONSTRAINT uq_user_user_id UNIQUE (user_id)
);

CREATE TRIGGER update_user_updated_at
    BEFORE UPDATE ON "user"
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- Role table
-- =====================================================
CREATE TABLE role (
    id          BIGSERIAL PRIMARY KEY,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  TIMESTAMPTZ NULL,

    CONSTRAINT uq_role_name UNIQUE (name)
);

CREATE TRIGGER update_role_updated_at
    BEFORE UPDATE ON role
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Seed ADMIN role for multi-user accounts
-- SUPER_ADMIN is created during initial onboarding, but ADMIN role needs to exist for creating additional users
INSERT INTO role (name, description, created_at, updated_at)
VALUES ('ADMIN', 'Admin role with full permissions except managing SUPER_ADMIN', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (name) DO UPDATE SET description = EXCLUDED.description;

-- =====================================================
-- Organization table
-- =====================================================
CREATE TABLE organization (
    id                      BIGSERIAL PRIMARY KEY,
    org_id                  VARCHAR(36) NOT NULL,
    name                    VARCHAR(255) NOT NULL,
    miner_auth_private_key  TEXT NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at              TIMESTAMPTZ NULL,

    CONSTRAINT uq_organization_org_id UNIQUE (org_id)
);

CREATE TRIGGER update_organization_updated_at
    BEFORE UPDATE ON organization
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- User Organization table (junction table)
-- =====================================================
CREATE TABLE user_organization (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL,
    organization_id BIGINT NOT NULL,
    role_id         BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at      TIMESTAMPTZ NULL,

    CONSTRAINT uq_user_organization UNIQUE (user_id, organization_id),
    CONSTRAINT fk_user_organization_user FOREIGN KEY (user_id)
        REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT fk_user_organization_organization FOREIGN KEY (organization_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_user_organization_role FOREIGN KEY (role_id)
        REFERENCES role(id) ON DELETE RESTRICT
);

CREATE TRIGGER update_user_organization_updated_at
    BEFORE UPDATE ON user_organization
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- Session table
-- =====================================================
CREATE TABLE session (
    id              BIGSERIAL PRIMARY KEY,
    session_id      VARCHAR(64) NOT NULL,
    user_id         BIGINT NOT NULL,
    organization_id BIGINT NOT NULL,
    user_agent      VARCHAR(512) NULL,
    ip_address      VARCHAR(45) NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_activity   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ NULL,

    CONSTRAINT uq_session_session_id UNIQUE (session_id),
    CONSTRAINT fk_session_user FOREIGN KEY (user_id)
        REFERENCES "user"(id) ON DELETE CASCADE,
    CONSTRAINT fk_session_organization FOREIGN KEY (organization_id)
        REFERENCES organization(id) ON DELETE CASCADE
);

CREATE INDEX idx_session_user_id ON session(user_id);
CREATE INDEX idx_session_expires_at ON session(expires_at);
