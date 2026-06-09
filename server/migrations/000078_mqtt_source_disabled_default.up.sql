ALTER TABLE curtailment_mqtt_source_config
    ALTER COLUMN enabled SET DEFAULT FALSE;

-- Prior MQTT ingest stamped source_actor_id as mqtt:<source_name>. The runtime
-- now stamps mqtt:<source_config_id> so source display-name edits do not orphan
-- active events. Rewrite non-terminal MQTT-owned events we can match.
UPDATE curtailment_event ce
SET source_actor_id = 'mqtt:' || cfg.id::TEXT
FROM curtailment_mqtt_source_config cfg
WHERE ce.org_id = cfg.organization_id
  AND ce.source_actor_type = 'webhook'
  AND ce.source_actor_id = 'mqtt:' || cfg.source_name
  AND ce.state IN ('pending', 'active', 'restoring');

COMMENT ON COLUMN curtailment_mqtt_source_config.enabled IS
    'Whether this MQTT source is actively ingested. New settings-created sources default disabled; existing rows are not backfilled.';
