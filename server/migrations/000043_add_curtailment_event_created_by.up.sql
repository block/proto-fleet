-- Reconciler dispatches Curtail / Uncurtail through command.Service, which
-- writes command_batch_log.created_by (BIGINT NOT NULL FK -> "user".id). Until
-- this column lands the reconciler had no operator id to attribute dispatch
-- to, so the FK would reject every reconciler-issued command. Capturing the
-- session.Info.UserID at Start time lets the reconciler dispatch under the
-- operator who initiated the event.
--
-- BE-2 wires PreviewCurtailmentPlan only and writes no rows to
-- curtailment_event, so NOT NULL with no backfill is safe.
ALTER TABLE curtailment_event
    ADD COLUMN created_by_user_id BIGINT NOT NULL,
    ADD CONSTRAINT fk_curtailment_event_created_by
        FOREIGN KEY (created_by_user_id) REFERENCES "user"(id);
