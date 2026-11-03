-- Importing existing intent is not a new hardware command or a verification
-- receipt. Keep its provenance without fabricating an actor or queued operation.
ALTER TABLE pool_configuration_assignment ALTER COLUMN operation_id DROP NOT NULL;
ALTER TABLE pool_configuration_assignment ADD COLUMN adopted_at TIMESTAMPTZ;
ALTER TABLE pool_configuration_assignment ADD CONSTRAINT chk_pool_configuration_assignment_origin
    CHECK (operation_id IS NOT NULL OR adopted_at IS NOT NULL);
-- Readback evidence is separate from the immutable application history.
-- A new assignment version cannot inherit a previous assignment's verification.
CREATE TABLE pool_configuration_observation (
    organization_id BIGINT NOT NULL,
    target_kind TEXT NOT NULL,
    target_id BIGINT NOT NULL,
    assignment_version BIGINT NOT NULL CHECK (assignment_version > 0),
    state TEXT NOT NULL CHECK (state IN ('verified','different','applied_unverified')),
    result_code TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    last_verified_at TIMESTAMPTZ,
    PRIMARY KEY (organization_id, target_kind, target_id),
    CONSTRAINT fk_pool_configuration_observation_assignment FOREIGN KEY (organization_id,target_kind,target_id)
        REFERENCES pool_configuration_assignment(organization_id,target_kind,target_id) ON DELETE CASCADE
);
