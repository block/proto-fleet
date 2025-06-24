CREATE TABLE queue_message
(
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    command_batch_log_id BIGINT NOT NULL,
    device_id BIGINT NOT NULL,
    command_type TEXT NOT NULL,
    status ENUM('PENDING', 'PROCESSING', 'SUCCESS', 'FAILED') NOT NULL,
    retry_count INT NOT NULL,
    error_info TEXT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    INDEX idx_device_status_created (device_id, status, created_at),
    INDEX idx_command_batch_log_id (command_batch_log_id),
    FOREIGN KEY (command_batch_log_id) REFERENCES command_batch_log(id),
    FOREIGN KEY (device_id) REFERENCES device(id)
);
