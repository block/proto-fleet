-- Preserve per-target phase outcomes for historical curtailment detail views.
-- The existing rolling cursor columns are reset when desired_state changes;
-- these summary columns survive that reset so a completed event can show both
-- curtail and restore outcomes from one activity row.
--
-- Existing rows are intentionally not backfilled. The phase summaries are
-- authoritative for rows updated after this migration; historical completed
-- events may show default/partial phase summaries rather than blocking startup
-- on a full curtailment_target table rewrite.
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

ALTER TABLE curtailment_target
    ADD CONSTRAINT ck_curtailment_target_curtail_state
        CHECK (curtail_state IN ('pending', 'dispatching', 'dispatched', 'confirmed', 'drifted', 'resolved', 'released', 'restore_failed')) NOT VALID,
    ADD CONSTRAINT ck_curtailment_target_restore_state
        CHECK (restore_state IS NULL OR restore_state IN ('pending', 'dispatching', 'dispatched', 'confirmed', 'drifted', 'resolved', 'released', 'restore_failed')) NOT VALID,
    ADD CONSTRAINT ck_curtailment_target_phase_counts
        CHECK (
            curtail_retry_count >= 0
            AND curtail_failure_count >= 0
            AND restore_retry_count >= 0
            AND restore_failure_count >= 0
        ) NOT VALID;
