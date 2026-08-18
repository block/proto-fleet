ALTER TABLE firmware_rollout_control
    ADD COLUMN actor_type TEXT NOT NULL DEFAULT 'user',
    ADD COLUMN actor_credential_id TEXT NULL,
    ADD CONSTRAINT ck_firmware_rollout_control_actor_type
        CHECK (actor_type IN ('user', 'api_key', 'system')),
    ADD CONSTRAINT ck_firmware_rollout_control_actor_credential
        CHECK (
            actor_credential_id IS NULL
            OR btrim(actor_credential_id) <> ''
        );

ALTER TABLE firmware_rollout_cause
    ADD COLUMN actor_type TEXT NOT NULL DEFAULT 'user',
    ADD COLUMN actor_credential_id TEXT NULL,
    ADD CONSTRAINT ck_firmware_rollout_cause_actor_type
        CHECK (actor_type IN ('user', 'api_key', 'system')),
    ADD CONSTRAINT ck_firmware_rollout_cause_actor_credential
        CHECK (
            actor_credential_id IS NULL
            OR btrim(actor_credential_id) <> ''
        );
