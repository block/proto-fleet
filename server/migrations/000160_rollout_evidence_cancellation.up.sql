ALTER TABLE firmware_rollout_batch
    DROP CONSTRAINT ck_firmware_rollout_batch_evidence_status,
    ADD COLUMN evidence_cancellation_reason TEXT NULL,
    ADD COLUMN evidence_cancelled_at TIMESTAMPTZ NULL,
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
            'finalized',
            'cancelled'
        )),
    ADD CONSTRAINT ck_firmware_rollout_batch_evidence_cancellation
        CHECK (
            (
                evidence_status = 'cancelled'
                AND evidence_cancellation_reason IS NOT NULL
                AND btrim(evidence_cancellation_reason) <> ''
                AND evidence_cancelled_at IS NOT NULL
                AND post_window_finalized
                AND post_window_finalized_at IS NOT NULL
            )
            OR
            (
                evidence_status <> 'cancelled'
                AND evidence_cancellation_reason IS NULL
                AND evidence_cancelled_at IS NULL
            )
        );

ALTER TABLE firmware_rollout_evidence
    ADD COLUMN status TEXT NOT NULL DEFAULT 'open',
    ADD COLUMN cancellation_reason TEXT NULL,
    ADD COLUMN cancelled_at TIMESTAMPTZ NULL,
    ADD CONSTRAINT ck_firmware_rollout_evidence_status
        CHECK (status IN ('open', 'completed', 'cancelled')),
    ADD CONSTRAINT ck_firmware_rollout_evidence_cancellation
        CHECK (
            (
                status = 'cancelled'
                AND cancellation_reason IS NOT NULL
                AND btrim(cancellation_reason) <> ''
                AND cancelled_at IS NOT NULL
            )
            OR
            (
                status <> 'cancelled'
                AND cancellation_reason IS NULL
                AND cancelled_at IS NULL
            )
        );

UPDATE firmware_rollout_evidence evidence
SET status = 'completed'
FROM firmware_rollout_member member,
     firmware_rollout_batch batch
WHERE member.id = evidence.member_id
  AND member.rollout_id = evidence.rollout_id
  AND member.org_id = evidence.org_id
  AND batch.id = member.batch_id
  AND batch.rollout_id = member.rollout_id
  AND batch.org_id = member.org_id
  AND batch.post_window_finalized;

UPDATE firmware_rollout_batch batch
SET evidence_status = 'cancelled',
    evidence_cancellation_reason = 'migration terminalized pre-existing aborted or reverted evidence',
    evidence_cancelled_at = COALESCE(
        rollout.aborted_at,
        rollout.reverted_at,
        rollout.updated_at
    ),
    evidence_error_message = 'migration terminalized pre-existing aborted or reverted evidence',
    healthy_since = NULL,
    evaluated_at = COALESCE(
        rollout.aborted_at,
        rollout.reverted_at,
        rollout.updated_at
    ),
    post_window_finalized = TRUE,
    post_window_finalized_at = COALESCE(
        rollout.aborted_at,
        rollout.reverted_at,
        rollout.updated_at
    )
FROM firmware_rollout rollout
WHERE rollout.id = batch.rollout_id
  AND rollout.org_id = batch.org_id
  AND rollout.state IN ('aborted', 'reverted')
  AND NOT batch.post_window_finalized;

UPDATE firmware_rollout_evidence evidence
SET status = 'cancelled',
    cancellation_reason = batch.evidence_cancellation_reason,
    cancelled_at = batch.evidence_cancelled_at
FROM firmware_rollout_member member,
     firmware_rollout_batch batch
WHERE member.id = evidence.member_id
  AND member.rollout_id = evidence.rollout_id
  AND member.org_id = evidence.org_id
  AND batch.id = member.batch_id
  AND batch.rollout_id = member.rollout_id
  AND batch.org_id = member.org_id
  AND batch.evidence_status = 'cancelled'
  AND evidence.status = 'open';

CREATE INDEX idx_firmware_rollout_evidence_open
    ON firmware_rollout_evidence(org_id, rollout_id, member_id)
    WHERE status = 'open';

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
                    AND grouped.source_channel_id = NEW.channel_id
                    AND child_member.state IN ('reverting', 'reverted')
                    AND child.state IN ('reverting', 'reverted')
              )
          )
    ) THEN
        RAISE EXCEPTION
            'active rollout lane model binding must match physical membership and declaration or frozen child transition'
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
