CREATE TABLE command_batch_log
(
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    uuid VARCHAR(36) NOT NULL,
    type text NOT NULL,
    created_by BIGINT NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    started_at TIMESTAMP(6),
    finished_at TIMESTAMP(6),
    status ENUM('PENDING', 'PROCESSING', 'FINISHED') NOT NULL,
    FOREIGN KEY (created_by) REFERENCES user(id),
    INDEX idx_created_by(created_by),
    INDEX idx_status(status),
    INDEX idx_type(type(42))
);
