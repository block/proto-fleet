DROP TABLE curtailment_automation_schedule;

-- Schedule rules cannot be represented without a schedule; remove them before
-- restoring the MQTT-only constraints.
DELETE FROM curtailment_automation_execution WHERE mqtt_source_id IS NULL;
ALTER TABLE curtailment_automation_execution ALTER COLUMN mqtt_source_id SET NOT NULL;

DELETE FROM curtailment_automation_rule WHERE trigger_type = 'SCHEDULE';
ALTER TABLE curtailment_automation_rule
    DROP CONSTRAINT ck_curtailment_automation_rule_trigger_type,
    ALTER COLUMN mqtt_source_id SET NOT NULL,
    ADD CONSTRAINT ck_curtailment_automation_rule_trigger_type CHECK ((trigger_type = 'MQTT'::text));
