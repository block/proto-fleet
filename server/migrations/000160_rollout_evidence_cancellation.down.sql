DROP INDEX IF EXISTS idx_firmware_rollout_evidence_open;

ALTER TABLE firmware_rollout_evidence
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_evidence_cancellation,
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_evidence_status,
    DROP COLUMN IF EXISTS cancelled_at,
    DROP COLUMN IF EXISTS cancellation_reason,
    DROP COLUMN IF EXISTS status;

UPDATE firmware_rollout_batch
SET evidence_status = 'finalized',
    evidence_cancellation_reason = NULL,
    evidence_cancelled_at = NULL
WHERE evidence_status = 'cancelled';

ALTER TABLE firmware_rollout_batch
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_batch_evidence_cancellation,
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_batch_evidence_status,
    DROP COLUMN IF EXISTS evidence_cancelled_at,
    DROP COLUMN IF EXISTS evidence_cancellation_reason,
    ADD CONSTRAINT ck_firmware_rollout_batch_evidence_status
        CHECK (evidence_status IN (
            'pending',
            'collecting',
            'unavailable',
            'observing',
            'healthy',
            'held',
            'stale',
            'automation_error',
            'finalized'
        ));

CREATE OR REPLACE FUNCTION validate_rollout_lane_model_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.ended_at IS NULL AND NOT EXISTS (
        SELECT 1
        FROM rollout_lane_model declaration
        JOIN device_set_membership membership
          ON membership.org_id = declaration.org_id
         AND membership.device_id = NEW.device_id
         AND membership.device_set_type = 'channel'
         AND membership.device_set_id = NEW.channel_id
        WHERE declaration.id = NEW.lane_model_id
          AND declaration.lane_id = NEW.lane_id
          AND declaration.org_id = NEW.org_id
          AND declaration.model_identity_key = NEW.model_identity_key
          AND (
              NEW.channel_id = declaration.current_channel_id
              OR EXISTS (
                  SELECT 1
                  FROM firmware_rollout_group_model grouped
                  JOIN firmware_rollout child
                    ON child.id = grouped.child_rollout_id
                   AND child.org_id = grouped.org_id
                  JOIN firmware_rollout_member child_member
                    ON child_member.rollout_id = child.id
                   AND child_member.org_id = child.org_id
                   AND child_member.device_id = NEW.device_id
                  WHERE grouped.lane_model_id = declaration.id
                    AND grouped.lane_id = declaration.lane_id
                    AND grouped.org_id = declaration.org_id
                    AND grouped.target_channel_id = NEW.channel_id
                    AND child_member.state = 'succeeded'
                    AND child.state IN (
                        'running',
                        'paused',
                        'review',
                        'completed',
                        'completed_with_failures'
                    )
              )
          )
    ) THEN
        RAISE EXCEPTION
            'active rollout lane model binding must match physical membership and declaration or frozen child target'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.ended_at IS NOT NULL AND EXISTS (
        SELECT 1
        FROM rollout_lane_topology_cutover cutover
        JOIN device_set_membership membership
          ON membership.org_id = NEW.org_id
         AND membership.device_id = NEW.device_id
         AND membership.device_set_type = 'channel'
         AND membership.device_set_id = NEW.channel_id
        WHERE cutover.org_id = NEW.org_id
          AND cutover.enabled
    ) THEN
        RAISE EXCEPTION
            'ended rollout lane model binding cannot retain physical membership'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
