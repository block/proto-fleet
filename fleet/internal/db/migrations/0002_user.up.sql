CREATE TABLE user
(
    id              BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id         TEXT   NOT NULL,
    username        TEXT   NOT NULL,
    password_hash   TEXT NOT NULL,
    created_at      TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);