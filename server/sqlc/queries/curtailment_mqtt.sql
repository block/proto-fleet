-- name: ListEnabledMQTTSources :many
-- Enabled MQTT sources, read once at subscriber startup. Enable/disable
-- takes effect on the next start (no hot reload).
SELECT *
FROM curtailment_mqtt_source_config
WHERE enabled = TRUE
ORDER BY id;

-- name: GetMQTTSourceConfigByID :one
SELECT *
FROM curtailment_mqtt_source_config
WHERE id = sqlc.arg('id');

-- name: GetMQTTSourceStateByID :one
SELECT *
FROM curtailment_mqtt_source_state
WHERE source_config_id = sqlc.arg('source_config_id');

-- name: UpsertMQTTSourceState :exec
-- Subscriber upserts state on each successful message receive (after
-- precedence dedup) and on each edge dispatch. Singleton per source.
INSERT INTO curtailment_mqtt_source_state (
    source_config_id,
    last_target,
    last_target_at,
    last_received_at,
    last_received_broker,
    last_edge_at,
    last_edge_event_uuid
) VALUES (
    sqlc.arg('source_config_id'),
    sqlc.narg('last_target'),
    sqlc.narg('last_target_at'),
    sqlc.narg('last_received_at'),
    sqlc.narg('last_received_broker'),
    sqlc.narg('last_edge_at'),
    sqlc.narg('last_edge_event_uuid')
)
ON CONFLICT (source_config_id) DO UPDATE
SET
    last_target            = COALESCE(EXCLUDED.last_target, curtailment_mqtt_source_state.last_target),
    last_target_at         = COALESCE(EXCLUDED.last_target_at, curtailment_mqtt_source_state.last_target_at),
    last_received_at       = COALESCE(EXCLUDED.last_received_at, curtailment_mqtt_source_state.last_received_at),
    last_received_broker   = COALESCE(EXCLUDED.last_received_broker, curtailment_mqtt_source_state.last_received_broker),
    last_edge_at           = COALESCE(EXCLUDED.last_edge_at, curtailment_mqtt_source_state.last_edge_at),
    last_edge_event_uuid   = COALESCE(EXCLUDED.last_edge_event_uuid, curtailment_mqtt_source_state.last_edge_event_uuid);

-- name: ListMQTTSourcesForWatchdog :many
-- Driven by the watchdog ticker: every enabled source paired with its
-- current state. NULL last_received_at signals cold-start; the subscriber
-- treats that as stale (fail-safe).
SELECT
    c.id                          AS source_config_id,
    c.source_name                 AS source_name,
    c.organization_id             AS organization_id,
    c.staleness_threshold_sec     AS staleness_threshold_sec,
    s.last_target                 AS last_target,
    s.last_received_at            AS last_received_at,
    s.last_edge_event_uuid        AS last_edge_event_uuid
FROM curtailment_mqtt_source_config c
LEFT JOIN curtailment_mqtt_source_state s ON s.source_config_id = c.id
WHERE c.enabled = TRUE
ORDER BY c.id;

-- name: InsertMQTTSourceConfig :one
-- Used by tests and operator-supplied DML. Production source rows are
-- seeded via migration data until the CRUD RPC lands.
INSERT INTO curtailment_mqtt_source_config (
    organization_id,
    service_user_id,
    source_name,
    topic,
    broker_primary_host,
    broker_secondary_host,
    broker_port,
    mqtt_username,
    mqtt_password_enc,
    contracted_curtailment_kw,
    scope_type,
    scope_device_identifiers,
    staleness_threshold_sec,
    min_curtailed_duration_sec,
    enabled
) VALUES (
    sqlc.arg('organization_id'),
    sqlc.arg('service_user_id'),
    sqlc.arg('source_name'),
    sqlc.arg('topic'),
    sqlc.arg('broker_primary_host'),
    sqlc.arg('broker_secondary_host'),
    sqlc.narg('broker_port'),
    sqlc.arg('mqtt_username'),
    sqlc.arg('mqtt_password_enc'),
    sqlc.arg('contracted_curtailment_kw'),
    sqlc.arg('scope_type'),
    sqlc.narg('scope_device_identifiers'),
    sqlc.narg('staleness_threshold_sec'),
    sqlc.narg('min_curtailed_duration_sec'),
    sqlc.arg('enabled')
)
RETURNING *;
