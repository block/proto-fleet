-- Miner errors table (single source of truth for hardware error incidents)
CREATE TABLE errors (
    id                 BIGINT PRIMARY KEY AUTO_INCREMENT,
    error_id           VARCHAR(26) NOT NULL UNIQUE,  -- ULID (time-sortable, external reference)
    org_id             BIGINT NOT NULL,
    miner_error        INT NOT NULL,
    severity           INT NOT NULL,
    summary            TEXT NOT NULL,                -- General description of the error
    impact             TEXT,
    cause_summary      TEXT,
    recommended_action TEXT,
    first_seen_at      TIMESTAMP(6) NOT NULL,
    last_seen_at       TIMESTAMP(6) NOT NULL,
    closed_at          TIMESTAMP(6),

    -- Device/component attribution
    device_id          BIGINT NOT NULL,
    component_id       VARCHAR(255),
    component_type     INT,

    -- Vendor metadata (all optional)
    vendor_code        VARCHAR(255),
    firmware           VARCHAR(255),
    extra              JSON,

    created_at         TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6),
    updated_at         TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),

    -- Indexes for common query patterns
    INDEX idx_dedup (org_id, device_id, miner_error, component_id, component_type),
    INDEX idx_org_miner_error (org_id, miner_error),
    INDEX idx_org_severity (org_id, severity),
    INDEX idx_org_last_seen (org_id, last_seen_at DESC),
    INDEX idx_org_component (org_id, component_id, component_type),
    INDEX idx_open_errors (org_id, closed_at, severity),
    INDEX idx_pagination (org_id, last_seen_at DESC, id DESC),

    CONSTRAINT fk_errors_organization FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_errors_device FOREIGN KEY (device_id) REFERENCES device(id) ON DELETE CASCADE
);
