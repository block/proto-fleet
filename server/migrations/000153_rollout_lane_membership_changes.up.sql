CREATE TABLE rollout_lane_membership_change (
    id UUID PRIMARY KEY,
    org_id BIGINT NOT NULL,
    target_lane_id UUID NOT NULL,
    authority_id UUID NULL,
    idempotency_key VARCHAR(256) NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    requested JSONB NOT NULL,
    applied JSONB NOT NULL,
    reason TEXT NOT NULL,
    actor_user_id BIGINT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_credential_id TEXT NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_rollout_lane_membership_change_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_membership_change_lane
        FOREIGN KEY (target_lane_id, org_id)
        REFERENCES rollout_lane(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_membership_change_authority
        FOREIGN KEY (authority_id, org_id)
        REFERENCES channel_firmware_authority(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_membership_change_actor
        FOREIGN KEY (actor_user_id) REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT uq_rollout_lane_membership_change_id_org UNIQUE (id, org_id),
    CONSTRAINT uq_rollout_lane_membership_change_idempotency
        UNIQUE (org_id, idempotency_key),
    CONSTRAINT uq_rollout_lane_membership_change_authority UNIQUE (authority_id),
    CONSTRAINT ck_rollout_lane_membership_change_idempotency
        CHECK (btrim(idempotency_key) <> ''),
    CONSTRAINT ck_rollout_lane_membership_change_fingerprint
        CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_rollout_lane_membership_change_requested
        CHECK (jsonb_typeof(requested) = 'object'),
    CONSTRAINT ck_rollout_lane_membership_change_applied
        CHECK (jsonb_typeof(applied) = 'object'),
    CONSTRAINT ck_rollout_lane_membership_change_reason
        CHECK (btrim(reason) <> ''),
    CONSTRAINT ck_rollout_lane_membership_change_actor_identity CHECK (
        actor_type = 'user'
            AND (
                actor_credential_id IS NULL
                OR btrim(actor_credential_id) <> ''
            )
        OR actor_type = 'api_key'
            AND actor_credential_id IS NOT NULL
            AND btrim(actor_credential_id) <> ''
        OR actor_type = 'system'
            AND actor_credential_id IS NULL
    )
);

CREATE INDEX idx_rollout_lane_membership_change_lane
    ON rollout_lane_membership_change(org_id, target_lane_id, applied_at DESC, id);

CREATE FUNCTION reject_rollout_lane_membership_change_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'rollout lane membership changes are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER rollout_lane_membership_change_immutable
    BEFORE UPDATE OR DELETE ON rollout_lane_membership_change
    FOR EACH ROW
    EXECUTE FUNCTION reject_rollout_lane_membership_change_mutation();

CREATE TRIGGER rollout_lane_membership_change_truncate_immutable
    BEFORE TRUNCATE ON rollout_lane_membership_change
    FOR EACH STATEMENT
    EXECUTE FUNCTION reject_rollout_lane_membership_change_mutation();
