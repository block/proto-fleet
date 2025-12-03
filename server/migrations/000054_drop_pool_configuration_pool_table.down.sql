CREATE TABLE pool_configuration_pool
(
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    pool_id BIGINT NOT NULL,
    pool_configuration_id BIGINT NOT NULL,
    priority INT NOT NULL,
    created_at TIMESTAMP(6) NOT NULL,
    updated_at TIMESTAMP(6) NOT NULL,

    CONSTRAINT fk_pool_configuration_pool_pool FOREIGN KEY (pool_id)
        REFERENCES pool (id) ON DELETE CASCADE,
    CONSTRAINT fk_pool_configuration_pool_configuration FOREIGN KEY (pool_configuration_id)
        REFERENCES pool_configuration (id) ON DELETE CASCADE,

    UNIQUE KEY uk_pool_configuration_pool (pool_configuration_id, pool_id),
    UNIQUE KEY uk_pool_configuration_priority (pool_configuration_id, priority),
    CONSTRAINT chk_priority_range CHECK (priority BETWEEN 0 AND 2),

    INDEX idx_pool_configuration_pool_pool_id (pool_id),
    INDEX idx_pool_configuration_pool_configuration_id (pool_configuration_id)
);
