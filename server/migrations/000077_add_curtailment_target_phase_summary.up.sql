-- Preserve per-target phase outcomes for historical curtailment detail views.
-- The existing rolling cursor columns are reset when desired_state changes;
-- these summary columns survive that reset so a completed event can show both
-- curtail and restore outcomes from one activity row.
ALTER TABLE curtailment_target
    ADD COLUMN curtail_state           TEXT        NOT NULL DEFAULT 'pending',
    ADD COLUMN curtail_dispatched_at   TIMESTAMPTZ NULL,
    ADD COLUMN curtail_batch_uuid      VARCHAR(36) NULL,
    ADD COLUMN curtail_completed_at    TIMESTAMPTZ NULL,
    ADD COLUMN curtail_retry_count     INT         NOT NULL DEFAULT 0,
    ADD COLUMN curtail_failure_count   INT         NOT NULL DEFAULT 0,
    ADD COLUMN curtail_last_error      TEXT        NULL,
    ADD COLUMN restore_state           TEXT        NULL,
    ADD COLUMN restore_started_at      TIMESTAMPTZ NULL,
    ADD COLUMN restore_dispatched_at   TIMESTAMPTZ NULL,
    ADD COLUMN restore_batch_uuid      VARCHAR(36) NULL,
    ADD COLUMN restore_completed_at    TIMESTAMPTZ NULL,
    ADD COLUMN restore_retry_count     INT         NOT NULL DEFAULT 0,
    ADD COLUMN restore_failure_count   INT         NOT NULL DEFAULT 0,
    ADD COLUMN restore_last_error      TEXT        NULL;

UPDATE curtailment_target AS ct
SET curtail_state = CASE
        WHEN ct.desired_state = 'curtailed' THEN ct.state
        WHEN ct.desired_state = 'active' AND ce.started_at IS NOT NULL THEN 'confirmed'
        ELSE 'pending'
    END,
    curtail_dispatched_at = CASE
        WHEN ct.desired_state = 'curtailed' THEN ct.last_dispatched_at
        ELSE NULL
    END,
    curtail_batch_uuid = CASE
        WHEN ct.desired_state = 'curtailed' THEN ct.last_batch_uuid
        ELSE NULL
    END,
    curtail_completed_at = CASE
        WHEN ct.desired_state = 'curtailed' AND ct.state IN ('confirmed', 'restore_failed') THEN ct.confirmed_at
        WHEN ct.desired_state = 'active' AND ce.started_at IS NOT NULL THEN ce.started_at
        ELSE NULL
    END,
    curtail_retry_count = CASE
        WHEN ct.desired_state = 'curtailed' THEN ct.retry_count
        ELSE 0
    END,
    curtail_failure_count = CASE
        WHEN ct.desired_state = 'curtailed' AND ct.last_error IS NOT NULL THEN GREATEST(ct.retry_count, 1)
        ELSE 0
    END,
    curtail_last_error = CASE
        WHEN ct.desired_state = 'curtailed' THEN ct.last_error
        ELSE NULL
    END,
    restore_state = CASE
        WHEN ct.desired_state = 'active' THEN ct.state
        ELSE NULL
    END,
    restore_started_at = CASE
        WHEN ct.desired_state = 'active' THEN COALESCE(ct.last_dispatched_at, ct.confirmed_at, ct.added_at)
        ELSE NULL
    END,
    restore_dispatched_at = CASE
        WHEN ct.desired_state = 'active' THEN ct.last_dispatched_at
        ELSE NULL
    END,
    restore_batch_uuid = CASE
        WHEN ct.desired_state = 'active' THEN ct.last_batch_uuid
        ELSE NULL
    END,
    restore_completed_at = CASE
        WHEN ct.desired_state = 'active' AND ct.state IN ('resolved', 'restore_failed', 'released') THEN ct.confirmed_at
        ELSE NULL
    END,
    restore_retry_count = CASE
        WHEN ct.desired_state = 'active' THEN ct.retry_count
        ELSE 0
    END,
    restore_failure_count = CASE
        WHEN ct.desired_state = 'active' AND ct.last_error IS NOT NULL THEN GREATEST(ct.retry_count, 1)
        ELSE 0
    END,
    restore_last_error = CASE
        WHEN ct.desired_state = 'active' THEN ct.last_error
        ELSE NULL
    END
FROM curtailment_event AS ce
WHERE ce.id = ct.curtailment_event_id;

ALTER TABLE curtailment_target
    ADD CONSTRAINT ck_curtailment_target_curtail_state
        CHECK (curtail_state IN ('pending', 'dispatching', 'dispatched', 'confirmed', 'drifted', 'resolved', 'released', 'restore_failed')),
    ADD CONSTRAINT ck_curtailment_target_restore_state
        CHECK (restore_state IS NULL OR restore_state IN ('pending', 'dispatching', 'dispatched', 'confirmed', 'drifted', 'resolved', 'released', 'restore_failed')),
    ADD CONSTRAINT ck_curtailment_target_phase_counts
        CHECK (
            curtail_retry_count >= 0
            AND curtail_failure_count >= 0
            AND restore_retry_count >= 0
            AND restore_failure_count >= 0
        );
