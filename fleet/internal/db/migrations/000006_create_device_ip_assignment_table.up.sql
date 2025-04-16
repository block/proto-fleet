CREATE TABLE device_ip_assignment
(
    id              BIGINT PRIMARY KEY AUTO_INCREMENT,
    device_id       BIGINT      NOT NULL,
    ip_address      VARCHAR(45) NOT NULL, -- Supports both IPv4 and IPv6
    assigned_at     TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6),
    unassigned_at   TIMESTAMP(6) NULL,
    is_current      BOOLEAN      DEFAULT TRUE,
    created_at      TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6),

    CONSTRAINT fk_device_ip_assignment_device_id FOREIGN KEY (device_id) REFERENCES device (id)
        ON DELETE RESTRICT,
    INDEX           idx_device_ip_assignment_device_current (device_id, is_current),
    INDEX           idx_device_ip_assignment_ip_address (ip_address)
);