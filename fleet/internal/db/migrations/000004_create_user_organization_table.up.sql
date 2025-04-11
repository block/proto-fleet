CREATE TABLE user_organization
(
    id              BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id         BIGINT       NOT NULL,
    organization_id BIGINT       NOT NULL,
    role_id         BIGINT       NOT NULL,
    created_at      TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleted_at      TIMESTAMP(6) NULL,

    CONSTRAINT uq_user_organization UNIQUE (user_id, organization_id),
    CONSTRAINT fk_user_organization_user FOREIGN KEY (user_id) REFERENCES `user`(id) 
        ON DELETE RESTRICT,
    CONSTRAINT fk_user_organization_organization FOREIGN KEY (organization_id) REFERENCES organization(id) 
        ON DELETE RESTRICT,
    CONSTRAINT fk_user_organization_role FOREIGN KEY (role_id) REFERENCES `role`(id) 
        ON DELETE RESTRICT
);
