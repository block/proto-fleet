CREATE TABLE pool
(
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    org_id        BIGINT       NOT NULL,
    pool_name     VARCHAR(255) NOT NULL,
    url           VARCHAR(255) NOT NULL,
    username      VARCHAR(255) NOT NULL,
    password_enc TEXT         NOT NULL,
    pool_status   ENUM('UNKNOWN', 'IDLE', 'ACTIVE', 'DEAD') NOT NULL,
    pool_priority INT          NOT NULL,
    is_default    BOOLEAN               DEFAULT FALSE,
    created_at    TIMESTAMP(6) NOT NULL,
    updated_at    TIMESTAMP(6) NOT NULL,
    deleted_at    TIMESTAMP(6) NULL,

    CONSTRAINT fk_pool_organization_organization FOREIGN KEY (org_id) REFERENCES organization (id)
        ON DELETE RESTRICT,
    INDEX         idx_pool_org_id_url (org_id, url)
);