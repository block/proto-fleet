-- Captures the operator's user.id at Start so the reconciler can dispatch
-- under it. command_batch_log.created_by has a NOT NULL FK to "user"(id);
-- without this column the reconciler's Curtail/Uncurtail would FK-fail.
--
-- NOT NULL without backfill is safe: PreviewCurtailmentPlan writes no rows
-- to curtailment_event, so the table is empty in any environment that has
-- only run earlier migrations.
ALTER TABLE curtailment_event
    ADD COLUMN created_by_user_id BIGINT NOT NULL,
    ADD CONSTRAINT fk_curtailment_event_created_by
        FOREIGN KEY (created_by_user_id) REFERENCES "user"(id);
