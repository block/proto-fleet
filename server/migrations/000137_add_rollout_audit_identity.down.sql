ALTER TABLE firmware_rollout_cause
    DROP COLUMN actor_credential_id,
    DROP COLUMN actor_type;

ALTER TABLE firmware_rollout_control
    DROP COLUMN actor_credential_id,
    DROP COLUMN actor_type;
