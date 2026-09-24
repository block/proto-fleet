-- Remaining pending generations are processed organization-wide by the old
-- worker after downgrade, so dropping target scope does not discard work.
DROP TABLE curtailment_rig_config_target;
ALTER TABLE curtailment_rig_config_reconciliation
    DROP COLUMN full_reconcile_generation;
