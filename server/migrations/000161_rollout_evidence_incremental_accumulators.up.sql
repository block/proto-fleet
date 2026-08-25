CREATE TABLE firmware_rollout_evidence_accumulator (
    rollout_id UUID NOT NULL,
    batch_id BIGINT NOT NULL,
    member_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL,
    processed_through TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NULL,
    hashrate_sum DOUBLE PRECISION NOT NULL DEFAULT 0,
    power_sum DOUBLE PRECISION NOT NULL DEFAULT 0,
    power_sample_count BIGINT NOT NULL DEFAULT 0,
    temperature_sum DOUBLE PRECISION NOT NULL DEFAULT 0,
    temperature_sample_count BIGINT NOT NULL DEFAULT 0,
    sample_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (member_id),
    CONSTRAINT fk_firmware_rollout_evidence_accumulator_member
        FOREIGN KEY (member_id, rollout_id, org_id)
        REFERENCES firmware_rollout_member(id, rollout_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_evidence_accumulator_batch
        FOREIGN KEY (batch_id, rollout_id, org_id)
        REFERENCES firmware_rollout_batch(id, rollout_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT ck_firmware_rollout_evidence_accumulator_counts
        CHECK (
            sample_count >= 0
            AND power_sample_count >= 0
            AND temperature_sample_count >= 0
        )
);

CREATE INDEX idx_firmware_rollout_evidence_accumulator_batch
    ON firmware_rollout_evidence_accumulator(org_id, rollout_id, batch_id, member_id);

CREATE TRIGGER update_firmware_rollout_evidence_accumulator_updated_at
    BEFORE UPDATE ON firmware_rollout_evidence_accumulator
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
