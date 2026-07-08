-- Validate the widened phase-state constraints separately from 000108 so the
-- full-table scan runs without the ACCESS EXCLUSIVE lock taken by the
-- constraint swap (mirrors the 000078/000079 split).
ALTER TABLE curtailment_target
    VALIDATE CONSTRAINT ck_curtailment_target_curtail_state,
    VALIDATE CONSTRAINT ck_curtailment_target_restore_state;
