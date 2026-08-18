ALTER TABLE rollout_lane
    ADD COLUMN deleted_at TIMESTAMPTZ NULL,
    ADD COLUMN deleted_by_user_id BIGINT NULL,
    ADD COLUMN deleted_actor_type TEXT NULL,
    ADD COLUMN deleted_actor_credential_id TEXT NULL,
    ADD COLUMN delete_reason TEXT NULL,
    ADD COLUMN delete_idempotency_key VARCHAR(256) NULL,
    ADD COLUMN delete_fingerprint VARCHAR(64) NULL,
    ADD CONSTRAINT fk_rollout_lane_deleted_by_user
        FOREIGN KEY (deleted_by_user_id) REFERENCES "user"(id) ON DELETE RESTRICT,
    ADD CONSTRAINT ck_rollout_lane_deleted_actor_identity CHECK (
        (
            deleted_actor_type IS NULL
            AND deleted_actor_credential_id IS NULL
        )
        OR deleted_actor_type = 'user'
            AND (
                deleted_actor_credential_id IS NULL
                OR btrim(deleted_actor_credential_id) <> ''
            )
        OR deleted_actor_type = 'api_key'
            AND deleted_actor_credential_id IS NOT NULL
            AND btrim(deleted_actor_credential_id) <> ''
        OR deleted_actor_type = 'system'
            AND deleted_actor_credential_id IS NULL
    ),
    ADD CONSTRAINT ck_rollout_lane_archive_metadata CHECK (
        (
            deleted_at IS NULL
            AND deleted_by_user_id IS NULL
            AND deleted_actor_type IS NULL
            AND deleted_actor_credential_id IS NULL
            AND delete_reason IS NULL
            AND delete_idempotency_key IS NULL
            AND delete_fingerprint IS NULL
        )
        OR
        (
            deleted_at IS NOT NULL
            AND deleted_by_user_id IS NOT NULL
            AND deleted_actor_type IS NOT NULL
            AND delete_reason IS NOT NULL
            AND btrim(delete_reason) <> ''
            AND delete_idempotency_key IS NOT NULL
            AND btrim(delete_idempotency_key) <> ''
            AND delete_fingerprint IS NOT NULL
            AND delete_fingerprint ~ '^[0-9a-f]{64}$'
        )
    );

ALTER TABLE rollout_lane
    DROP CONSTRAINT uq_rollout_lane_label;

CREATE UNIQUE INDEX uq_rollout_lane_label
    ON rollout_lane(org_id, label)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX uq_rollout_lane_delete_idempotency
    ON rollout_lane(org_id, delete_idempotency_key)
    WHERE delete_idempotency_key IS NOT NULL;
