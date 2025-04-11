CREATE TABLE `user`
(
    id            BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id       VARCHAR(36)  NOT NULL,
    username      VARCHAR(255) NOT NULL,
    password_hash TEXT         NOT NULL,
    created_at    TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleted_at    TIMESTAMP(6) NULL,

    CONSTRAINT uq_user_username UNIQUE (username(255)),
    CONSTRAINT uq_user_user_id UNIQUE (user_id(36))
);
