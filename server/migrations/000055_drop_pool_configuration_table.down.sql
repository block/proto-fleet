CREATE TABLE pool_configuration
(
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    org_id        BIGINT       NOT NULL,
    name          VARCHAR(255) NOT NULL,
    description   TEXT,
    created_at    TIMESTAMP(6) NOT NULL,
    updated_at    TIMESTAMP(6) NOT NULL,

    CONSTRAINT fk_pool_configuration_organization FOREIGN KEY (org_id) REFERENCES organization (id)
        ON DELETE RESTRICT,
    INDEX         idx_pool_configuration_org_id (org_id)
);
