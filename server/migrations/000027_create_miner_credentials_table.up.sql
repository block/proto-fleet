CREATE TABLE miner_credentials (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    device_id BIGINT NOT NULL,
    username_enc TEXT NOT NULL, -- Encrypted username
    password_enc TEXT NOT NULL, -- Encrypted password
    created_at TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),

    CONSTRAINT fk_miner_credentials_device_id FOREIGN KEY (device_id) REFERENCES device(id) ON DELETE CASCADE,
    CONSTRAINT uq_miner_credentials_device_id UNIQUE (device_id),
    INDEX idx_miner_credentials_device_id (device_id)
);