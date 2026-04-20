-- Proto Fleet PostgreSQL Device Tables
-- Creates discovered_device, device, device_status, device_pairing, miner_credentials

-- =====================================================
-- Discovered Device table
-- Represents a device found during network discovery
-- =====================================================
CREATE TABLE discovered_device (
    id                   BIGSERIAL PRIMARY KEY,
    org_id               BIGINT NOT NULL,
    device_identifier    VARCHAR(255) NOT NULL,
    model                VARCHAR(255) NULL,
    manufacturer         VARCHAR(255) NULL,
    type                 VARCHAR(50) NOT NULL,
    firmware_version     VARCHAR(255) NULL,
    ip_address           VARCHAR(45) NOT NULL,
    port                 VARCHAR(10) NOT NULL,
    url_scheme           VARCHAR(10) NOT NULL,
    discovery_metadata   TEXT NULL,
    first_discovered     TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    last_seen            TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    is_active            BOOLEAN NOT NULL DEFAULT FALSE,
    created_at           TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    deleted_at           TIMESTAMPTZ NULL,

    CONSTRAINT fk_discovered_device_org FOREIGN KEY (org_id)
        REFERENCES organization(id),
    CONSTRAINT uk_discovered_device_org_identifier UNIQUE (org_id, device_identifier)
);

CREATE INDEX idx_discovered_device_org ON discovered_device(org_id);
CREATE INDEX idx_discovered_device_type ON discovered_device(org_id, type);
CREATE INDEX idx_discovered_device_org_active ON discovered_device(org_id, is_active, deleted_at);
CREATE INDEX idx_discovered_device_ip ON discovered_device(ip_address, port, url_scheme);
CREATE INDEX idx_discovered_device_identifier ON discovered_device(device_identifier);

CREATE TRIGGER update_discovered_device_updated_at
    BEFORE UPDATE ON discovered_device
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_discovered_device_last_seen
    BEFORE UPDATE ON discovered_device
    FOR EACH ROW
    EXECUTE FUNCTION update_last_seen_column();

-- =====================================================
-- Device table
-- Represents a paired/registered device in the fleet
-- =====================================================
CREATE TABLE device (
    id                   BIGSERIAL PRIMARY KEY,
    device_identifier    VARCHAR(36) NOT NULL,
    mac_address          VARCHAR(17) NOT NULL,
    serial_number        VARCHAR(255),
    org_id               BIGINT NOT NULL,
    discovered_device_id BIGINT NOT NULL,
    created_at           TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    deleted_at           TIMESTAMPTZ NULL,

    CONSTRAINT uq_device_device_identifier UNIQUE (device_identifier),
    CONSTRAINT uq_device_serial_number UNIQUE (serial_number),
    CONSTRAINT fk_device_organization FOREIGN KEY (org_id)
        REFERENCES organization(id),
    CONSTRAINT fk_device_discovered_device FOREIGN KEY (discovered_device_id)
        REFERENCES discovered_device(id)
);

CREATE INDEX idx_device_discovered_device_id ON device(discovered_device_id);

CREATE TRIGGER update_device_updated_at
    BEFORE UPDATE ON device
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- Device Status table
-- Tracks the current operational status of each device
-- =====================================================
CREATE TABLE device_status (
    id               BIGSERIAL PRIMARY KEY,
    device_id        BIGINT NOT NULL,
    status           device_status_enum NOT NULL DEFAULT 'ACTIVE',
    status_timestamp TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    status_details   TEXT,
    created_at       TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_device_status_device_id FOREIGN KEY (device_id)
        REFERENCES device(id),
    CONSTRAINT ux_device_status_device_id UNIQUE (device_id)
);

CREATE INDEX idx_device_status_device_id_status_timestamp ON device_status(device_id, status_timestamp);

-- =====================================================
-- Device Pairing table
-- Tracks the pairing status of each device
-- =====================================================
CREATE TABLE device_pairing (
    id             BIGSERIAL PRIMARY KEY,
    device_id      BIGINT NOT NULL,
    pairing_token  VARCHAR(255),
    pairing_status pairing_status_enum NOT NULL,
    paired_at      TIMESTAMPTZ NULL,
    unpaired_at    TIMESTAMPTZ NULL,
    created_at     TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_device_pairing_device_id FOREIGN KEY (device_id)
        REFERENCES device(id),
    CONSTRAINT uq_device_pairing_device_id UNIQUE (device_id)
);

CREATE INDEX idx_device_pairing_device_id_pairing_status ON device_pairing(device_id, pairing_status);

CREATE TRIGGER update_device_pairing_updated_at
    BEFORE UPDATE ON device_pairing
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- Miner Credentials table
-- Stores encrypted credentials for accessing miners
-- =====================================================
CREATE TABLE miner_credentials (
    id           BIGSERIAL PRIMARY KEY,
    device_id    BIGINT NOT NULL,
    username_enc TEXT NOT NULL,
    password_enc TEXT NOT NULL,
    created_at   TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at   TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_miner_credentials_device_id FOREIGN KEY (device_id)
        REFERENCES device(id) ON DELETE CASCADE,
    CONSTRAINT uq_miner_credentials_device_id UNIQUE (device_id)
);

CREATE INDEX idx_miner_credentials_device_id ON miner_credentials(device_id);

CREATE TRIGGER update_miner_credentials_updated_at
    BEFORE UPDATE ON miner_credentials
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
