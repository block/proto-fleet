-- Proto Fleet PostgreSQL Pool and Command Tables
-- Creates pool, command_batch_log, command_on_device_log, queue_message

-- =====================================================
-- Pool table
-- Mining pool configuration
-- =====================================================
CREATE TABLE pool (
    id           BIGSERIAL PRIMARY KEY,
    org_id       BIGINT NOT NULL,
    pool_name    VARCHAR(255) NOT NULL,
    url          VARCHAR(255) NOT NULL,
    username     VARCHAR(255) NOT NULL,
    password_enc TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at   TIMESTAMPTZ NULL,

    CONSTRAINT fk_pool_organization FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT uk_pool_org_url_username UNIQUE (org_id, url, username)
);

CREATE INDEX idx_pool_org_id_url ON pool(org_id, url);

-- =====================================================
-- Command Batch Log table
-- Tracks batches of commands sent to devices
-- =====================================================
CREATE TABLE command_batch_log (
    id            BIGSERIAL PRIMARY KEY,
    uuid          VARCHAR(36) NOT NULL,
    type          TEXT NOT NULL,
    created_by    BIGINT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    status        batch_status_enum NOT NULL,
    devices_count INT NOT NULL DEFAULT 0,
    payload       JSONB NULL,

    CONSTRAINT fk_command_batch_log_created_by FOREIGN KEY (created_by)
        REFERENCES "user"(id)
);

CREATE UNIQUE INDEX idx_command_batch_log_uuid ON command_batch_log(uuid);
CREATE INDEX idx_command_batch_log_created_by ON command_batch_log(created_by);
CREATE INDEX idx_command_batch_log_status ON command_batch_log(status);
CREATE INDEX idx_command_batch_log_type ON command_batch_log(type);

-- =====================================================
-- Command On Device Log table
-- Tracks individual command results per device
-- =====================================================
CREATE TABLE command_on_device_log (
    id                   BIGSERIAL PRIMARY KEY,
    command_batch_log_id BIGINT NOT NULL,
    device_id            BIGINT NOT NULL,
    status               device_command_status_enum NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_command_on_device_log_batch FOREIGN KEY (command_batch_log_id)
        REFERENCES command_batch_log(id),
    CONSTRAINT fk_command_on_device_log_device FOREIGN KEY (device_id)
        REFERENCES device(id),
    CONSTRAINT unique_batch_device UNIQUE (command_batch_log_id, device_id)
);

CREATE INDEX idx_command_on_device_log_batch_id ON command_on_device_log(command_batch_log_id);
CREATE INDEX idx_command_on_device_log_device_id ON command_on_device_log(device_id);

CREATE TRIGGER update_command_on_device_log_updated_at
    BEFORE UPDATE ON command_on_device_log
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- Queue Message table
-- Message queue for async command processing
-- =====================================================
CREATE TABLE queue_message (
    id                     BIGSERIAL PRIMARY KEY,
    command_batch_log_uuid VARCHAR(36) NOT NULL,
    device_id              BIGINT NOT NULL,
    command_type           TEXT NOT NULL,
    status                 queue_status_enum NOT NULL,
    retry_count            INT NOT NULL,
    error_info             TEXT NULL,
    payload                JSONB NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_queue_message_device FOREIGN KEY (device_id)
        REFERENCES device(id)
);

CREATE INDEX idx_queue_message_device_status_created ON queue_message(device_id, status, created_at);
CREATE INDEX idx_queue_message_batch_uuid ON queue_message(command_batch_log_uuid);

CREATE TRIGGER update_queue_message_updated_at
    BEFORE UPDATE ON queue_message
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
