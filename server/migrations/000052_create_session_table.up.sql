CREATE TABLE session (
    id              BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    session_id      VARCHAR(64)  NOT NULL COMMENT 'Cryptographically secure random session identifier',
    user_id         BIGINT       NOT NULL COMMENT 'FK to user.id',
    organization_id BIGINT       NOT NULL COMMENT 'FK to organization.id (cached for performance)',
    user_agent      VARCHAR(512) NULL COMMENT 'Browser/client identifier',
    ip_address      VARCHAR(45)  NULL COMMENT 'Client IP (supports IPv6)',
    created_at      TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    last_activity   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT 'Updated on each request for sliding window',
    expires_at      TIMESTAMP(6) NOT NULL COMMENT 'Session expiry time',
    revoked_at      TIMESTAMP(6) NULL COMMENT 'Non-null if session was explicitly invalidated',

    CONSTRAINT uq_session_session_id UNIQUE (session_id),
    CONSTRAINT fk_session_user FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE,
    CONSTRAINT fk_session_organization FOREIGN KEY (organization_id) REFERENCES organization(id) ON DELETE CASCADE,

    INDEX idx_session_user_id (user_id),
    INDEX idx_session_expires_at (expires_at)
);
