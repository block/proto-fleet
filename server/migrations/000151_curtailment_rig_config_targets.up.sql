-- Keep the original outbox's row shape and writes compatible with older fleetd
-- instances during HA upgrades. A generation without a marker requires a full
-- reconciliation, including requests written by an older instance.
CREATE TABLE curtailment_rig_config_target_generation (
    organization_id BIGINT NOT NULL,
    generation      BIGINT NOT NULL CHECK (generation > 0),
    PRIMARY KEY (organization_id, generation),
    FOREIGN KEY (organization_id)
        REFERENCES curtailment_rig_config_reconciliation (organization_id) ON DELETE CASCADE
);

CREATE TABLE curtailment_rig_config_target (
    organization_id      BIGINT NOT NULL,
    device_id            BIGINT NOT NULL,
    requested_generation BIGINT NOT NULL CHECK (requested_generation > 0),
    PRIMARY KEY (organization_id, device_id),
    FOREIGN KEY (organization_id)
        REFERENCES curtailment_rig_config_reconciliation (organization_id) ON DELETE CASCADE,
    FOREIGN KEY (device_id) REFERENCES device (id) ON DELETE CASCADE
);
