CREATE TABLE device_status (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    device_id BIGINT NOT NULL,
    status ENUM('ONLINE', 'OFFLINE', 'MAINTENANCE', 'ERROR') NOT NULL,
    status_timestamp TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6),
    status_details TEXT,
    created_at TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6),
    
    CONSTRAINT fk_device_status_device_id FOREIGN KEY (device_id) REFERENCES device(id),
    INDEX idx_device_status_device_id_status_timestamp (device_id, status_timestamp)
);