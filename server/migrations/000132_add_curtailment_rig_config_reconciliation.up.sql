CREATE TABLE curtailment_rig_config_reconciliation (
    organization_id     BIGINT      PRIMARY KEY,
    requested_by        BIGINT      NOT NULL,
    desired_generation  BIGINT      NOT NULL DEFAULT 1,
    enqueued_generation  BIGINT      NOT NULL DEFAULT 0,
    retry_at            TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    lease_expires_at    TIMESTAMPTZ NULL,
    last_error          TEXT        NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_curtailment_rig_config_reconciliation_org
        FOREIGN KEY (organization_id) REFERENCES organization(id) ON DELETE CASCADE,
    CONSTRAINT fk_curtailment_rig_config_reconciliation_user
        FOREIGN KEY (requested_by) REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT ck_curtailment_rig_config_reconciliation_generation
        CHECK (desired_generation > 0
            AND enqueued_generation >= 0
            AND enqueued_generation <= desired_generation)
);

-- Existing enabled sources predate the reconciliation outbox. Seed one due
-- request per organization so upgrades deliver their current fallback config
-- without waiting for a settings edit or a new rig pairing.
INSERT INTO curtailment_rig_config_reconciliation (
    organization_id,
    requested_by
)
SELECT DISTINCT ON (organization_id)
    organization_id,
    service_user_id
FROM curtailment_mqtt_source_config
WHERE enabled = TRUE
ORDER BY organization_id, id;

CREATE INDEX idx_curtailment_rig_config_reconciliation_due
    ON curtailment_rig_config_reconciliation (retry_at, organization_id);

CREATE TRIGGER update_curtailment_rig_config_reconciliation_updated_at
    BEFORE UPDATE ON curtailment_rig_config_reconciliation
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
