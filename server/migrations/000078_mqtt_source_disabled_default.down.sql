ALTER TABLE curtailment_mqtt_source_config
    ALTER COLUMN enabled SET DEFAULT TRUE;

UPDATE curtailment_event ce
SET source_actor_id = 'mqtt:' || cfg.source_name
FROM curtailment_mqtt_source_config cfg
WHERE ce.org_id = cfg.organization_id
  AND ce.source_actor_type = 'webhook'
  AND ce.source_actor_id = 'mqtt:' || cfg.id::TEXT
  AND ce.state IN ('pending', 'active', 'restoring');

COMMENT ON COLUMN curtailment_mqtt_source_config.enabled IS NULL;
