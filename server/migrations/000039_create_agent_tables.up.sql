ALTER TABLE device
    ADD CONSTRAINT uq_device_id_org_id UNIQUE (id, org_id);

CREATE TABLE agent (
    id                   BIGSERIAL PRIMARY KEY,
    org_id               BIGINT NOT NULL,
    name                 VARCHAR(255) NOT NULL,
    identity_pubkey      BYTEA NOT NULL,
    miner_signing_pubkey BYTEA NOT NULL,
    enrollment_status    VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    last_seen_at         TIMESTAMPTZ NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at           TIMESTAMPTZ NULL,

    CONSTRAINT fk_agent_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT uq_agent_id_org_id UNIQUE (id, org_id),
    CONSTRAINT ck_agent_enrollment_status
        CHECK (enrollment_status IN ('PENDING', 'CONFIRMED', 'REVOKED'))
);

CREATE INDEX idx_agent_org_id ON agent(org_id);

CREATE UNIQUE INDEX uq_agent_identity_pubkey
    ON agent(identity_pubkey)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX uq_agent_org_name
    ON agent(org_id, name)
    WHERE deleted_at IS NULL;

CREATE TRIGGER update_agent_updated_at
    BEFORE UPDATE ON agent
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE agent_device (
    agent_id    BIGINT NOT NULL,
    device_id   BIGINT NOT NULL,
    org_id      BIGINT NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    assigned_by BIGINT NULL,

    PRIMARY KEY (agent_id, device_id),
    CONSTRAINT fk_agent_device_agent FOREIGN KEY (agent_id, org_id)
        REFERENCES agent(id, org_id) ON DELETE CASCADE,
    CONSTRAINT fk_agent_device_device FOREIGN KEY (device_id, org_id)
        REFERENCES device(id, org_id) ON DELETE CASCADE,
    CONSTRAINT fk_agent_device_assigned_by FOREIGN KEY (assigned_by)
        REFERENCES "user"(id) ON DELETE SET NULL,
    CONSTRAINT uq_agent_device_device_id UNIQUE (device_id)
);

CREATE INDEX idx_agent_device_org_id ON agent_device(org_id);
