-- Validate phase-summary constraints separately from the additive column
-- migration so deploy-time validation is isolated from schema expansion.
ALTER TABLE curtailment_target
    VALIDATE CONSTRAINT ck_curtailment_target_curtail_state,
    VALIDATE CONSTRAINT ck_curtailment_target_restore_state,
    VALIDATE CONSTRAINT ck_curtailment_target_phase_counts;
