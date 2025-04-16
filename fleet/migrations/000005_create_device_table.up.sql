CREATE TABLE device
(
    id                BIGINT PRIMARY KEY AUTO_INCREMENT,
    device_identifier VARCHAR(36), -- identifier to be used externally
    mac_address       VARCHAR(17) NOT NULL, -- Format: XX:XX:XX:XX:XX:XX
    device_name       VARCHAR(100),
    manufacturer      VARCHAR(100),
    model             VARCHAR(100),
    serial_number     VARCHAR(100),
    first_discovered  TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6),
    last_seen         TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    is_active         BOOLEAN      DEFAULT TRUE,
    created_at        TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6),
    updated_at        TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),

    CONSTRAINT device_identifier UNIQUE uq_device_device_identifier (device_identifier),
    CONSTRAINT mac_address UNIQUE uq_device_mac_address (mac_address),
    CONSTRAINT serial_number UNIQUE uq_device_serial_number (serial_number)
);