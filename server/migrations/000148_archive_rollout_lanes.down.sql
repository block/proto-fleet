DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM rollout_lane
        GROUP BY org_id, label
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION
            'cannot restore uq_rollout_lane_label while archived label reuse creates duplicate organization labels';
    END IF;
END;
$$;

DROP INDEX IF EXISTS uq_rollout_lane_delete_idempotency;
DROP INDEX IF EXISTS uq_rollout_lane_label;

ALTER TABLE rollout_lane
    DROP CONSTRAINT IF EXISTS ck_rollout_lane_archive_metadata,
    DROP CONSTRAINT IF EXISTS ck_rollout_lane_deleted_actor_identity,
    DROP CONSTRAINT IF EXISTS fk_rollout_lane_deleted_by_user,
    DROP COLUMN IF EXISTS deleted_at,
    DROP COLUMN IF EXISTS deleted_by_user_id,
    DROP COLUMN IF EXISTS deleted_actor_type,
    DROP COLUMN IF EXISTS deleted_actor_credential_id,
    DROP COLUMN IF EXISTS delete_reason,
    DROP COLUMN IF EXISTS delete_idempotency_key,
    DROP COLUMN IF EXISTS delete_fingerprint,
    ADD CONSTRAINT uq_rollout_lane_label UNIQUE (org_id, label);
