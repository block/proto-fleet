-- Pre-existing requests were created for organization-wide delivery. Preserve
-- their scope when adding device-targeted requests to the outbox.
ALTER TABLE curtailment_rig_config_reconciliation
    ADD COLUMN full_reconcile_generation BIGINT NOT NULL DEFAULT 0;

UPDATE curtailment_rig_config_reconciliation
SET full_reconcile_generation = desired_generation;

ALTER TABLE curtailment_rig_config_reconciliation
    ADD CONSTRAINT ck_curtailment_rig_config_reconciliation_full_generation
        CHECK (full_reconcile_generation >= 0
            AND full_reconcile_generation <= desired_generation);

CREATE TABLE curtailment_rig_config_target (
    organization_id      BIGINT NOT NULL,
    device_id            BIGINT NOT NULL,
    requested_generation BIGINT NOT NULL CHECK (requested_generation > 0),
    PRIMARY KEY (organization_id, device_id),
    FOREIGN KEY (organization_id)
        REFERENCES curtailment_rig_config_reconciliation (organization_id) ON DELETE CASCADE,
    FOREIGN KEY (device_id) REFERENCES device (id) ON DELETE CASCADE
);
