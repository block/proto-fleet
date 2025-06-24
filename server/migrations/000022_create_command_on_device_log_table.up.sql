CREATE TABLE command_on_device_log
(
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    command_batch_log_id BIGINT NOT NULL,
    device_id BIGINT NOT NULL,
    status ENUM('SUCCESS', 'FAILED') NOT NULL,
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    FOREIGN KEY (command_batch_log_id) REFERENCES command_batch_log(id),
    FOREIGN KEY (device_id) REFERENCES device(id),
    INDEX idx_command_log_id(command_batch_log_id),
    INDEX idx_device_id(device_id),
    CONSTRAINT unique_batch_device UNIQUE (command_batch_log_id, device_id)
);
