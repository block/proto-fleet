-- Remaining pending generations are processed organization-wide by the old
-- worker after downgrade, so dropping target scope does not discard work.
DROP TABLE curtailment_rig_config_target;
DROP TABLE curtailment_rig_config_target_generation;
