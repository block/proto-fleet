DROP TABLE IF EXISTS curtailment_automation_rule_state;
DROP TABLE IF EXISTS curtailment_automation_rule;

ALTER TABLE curtailment_response_profile
    DROP CONSTRAINT IF EXISTS uq_curtailment_response_profile_id_org;

ALTER TABLE curtailment_mqtt_source_config
    DROP CONSTRAINT IF EXISTS uq_curtailment_mqtt_source_config_id_org;

ALTER TABLE curtailment_event
    DROP CONSTRAINT IF EXISTS ck_curtailment_event_curtail_batch_interval,
    DROP CONSTRAINT IF EXISTS ck_curtailment_event_curtail_batch_size,
    DROP COLUMN IF EXISTS curtail_batch_interval_sec,
    DROP COLUMN IF EXISTS curtail_batch_size;
