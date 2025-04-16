CREATE TABLE device_pairing (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    device_id BIGINT NOT NULL,
    pairing_token VARCHAR(255),
    pairing_status ENUM('PENDING', 'PAIRED', 'UNPAIRED', 'FAILED') NOT NULL,
    paired_at TIMESTAMP(6) NULL,
    unpaired_at TIMESTAMP(6) NULL,
    created_at TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    
    CONSTRAINT fk_device_pairing_device_id FOREIGN KEY (device_id) REFERENCES device(id),
    INDEX idx_device_pairing_device_id_pairing_status (device_id, pairing_status)
);