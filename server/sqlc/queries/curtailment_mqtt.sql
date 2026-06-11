-- name: ListEnabledMQTTSources :many
-- Enabled MQTT sources for subscriber reconciliation.
SELECT *
FROM curtailment_mqtt_source_config
WHERE enabled = TRUE
ORDER BY id;

-- name: ListMQTTSourceConfigsByOrg :many
SELECT *
FROM curtailment_mqtt_source_config
WHERE organization_id = sqlc.arg('organization_id')
ORDER BY id;

-- name: GetMQTTSourceConfigByOrg :one
SELECT *
FROM curtailment_mqtt_source_config
WHERE id = sqlc.arg('id')
  AND organization_id = sqlc.arg('organization_id');

-- name: GetMQTTSourceStateByID :one
SELECT *
FROM curtailment_mqtt_source_state
WHERE source_config_id = sqlc.arg('source_config_id');

-- name: ListMQTTSourceStatesByOrg :many
SELECT st.*
FROM curtailment_mqtt_source_state st
JOIN curtailment_mqtt_source_config cfg
  ON cfg.id = st.source_config_id
WHERE cfg.organization_id = sqlc.arg('organization_id')
ORDER BY st.source_config_id;

-- name: UpsertMQTTSourceState :exec
-- Subscriber upserts source signal state on each successful message receive,
-- after precedence/dedup processing. Singleton per source.
INSERT INTO curtailment_mqtt_source_state (
    source_config_id,
    last_target,
    last_target_at,
    last_processed_target,
    last_processed_targets,
    last_received_at,
    last_received_broker,
    last_edge_at,
    pending_direction,
    pending_target,
    pending_target_at,
    pending_received_at,
    pending_received_broker,
    pending_prior_edge_at,
    pending_retry_at
) VALUES (
    sqlc.arg('source_config_id'),
    sqlc.narg('last_target'),
    sqlc.narg('last_target_at'),
    sqlc.narg('last_processed_target'),
    sqlc.narg('last_processed_targets'),
    sqlc.narg('last_received_at'),
    sqlc.narg('last_received_broker'),
    sqlc.narg('last_edge_at'),
    sqlc.narg('pending_direction'),
    sqlc.narg('pending_target'),
    sqlc.narg('pending_target_at'),
    sqlc.narg('pending_received_at'),
    sqlc.narg('pending_received_broker'),
    sqlc.narg('pending_prior_edge_at'),
    sqlc.narg('pending_retry_at')
)
ON CONFLICT (source_config_id) DO UPDATE
SET
    last_target            = EXCLUDED.last_target,
    last_target_at         = EXCLUDED.last_target_at,
    last_processed_target  = EXCLUDED.last_processed_target,
    last_processed_targets = EXCLUDED.last_processed_targets,
    last_received_at       = EXCLUDED.last_received_at,
    last_received_broker   = EXCLUDED.last_received_broker,
    last_edge_at           = EXCLUDED.last_edge_at,
    pending_direction      = EXCLUDED.pending_direction,
    pending_target         = EXCLUDED.pending_target,
    pending_target_at      = EXCLUDED.pending_target_at,
    pending_received_at    = EXCLUDED.pending_received_at,
    pending_received_broker = EXCLUDED.pending_received_broker,
    pending_prior_edge_at  = EXCLUDED.pending_prior_edge_at,
    pending_retry_at       = EXCLUDED.pending_retry_at;

-- name: InsertMQTTSourceConfig :one
INSERT INTO curtailment_mqtt_source_config (
    organization_id,
    service_user_id,
    source_name,
    topic,
    broker_primary_host,
    broker_secondary_host,
    broker_port,
    broker_transport,
    mqtt_username,
    mqtt_password_enc,
    payload_format,
    staleness_threshold_sec,
    enabled
) VALUES (
    sqlc.arg('organization_id'),
    sqlc.arg('service_user_id'),
    sqlc.arg('source_name'),
    sqlc.arg('topic'),
    sqlc.arg('broker_primary_host'),
    sqlc.arg('broker_secondary_host'),
    sqlc.narg('broker_port'),
    sqlc.arg('broker_transport'),
    sqlc.arg('mqtt_username'),
    sqlc.arg('mqtt_password_enc'),
    sqlc.arg('payload_format'),
    sqlc.narg('staleness_threshold_sec'),
    sqlc.arg('enabled')
)
RETURNING *;

-- name: UpdateMQTTSourceConfig :one
UPDATE curtailment_mqtt_source_config
SET
    service_user_id = sqlc.arg('service_user_id'),
    source_name = sqlc.arg('source_name'),
    topic = sqlc.arg('topic'),
    broker_primary_host = sqlc.arg('broker_primary_host'),
    broker_secondary_host = sqlc.arg('broker_secondary_host'),
    broker_port = sqlc.narg('broker_port'),
    broker_transport = sqlc.arg('broker_transport'),
    mqtt_username = sqlc.arg('mqtt_username'),
    mqtt_password_enc = sqlc.arg('mqtt_password_enc'),
    payload_format = sqlc.arg('payload_format'),
    staleness_threshold_sec = sqlc.narg('staleness_threshold_sec')
WHERE id = sqlc.arg('id')
  AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- name: SetMQTTSourceConfigEnabled :one
UPDATE curtailment_mqtt_source_config
SET enabled = sqlc.arg('enabled')
WHERE id = sqlc.arg('id')
  AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- name: DeleteDisabledMQTTSourceConfigByOrg :execrows
DELETE FROM curtailment_mqtt_source_config
WHERE id = sqlc.arg('id')
  AND organization_id = sqlc.arg('organization_id')
  AND enabled = FALSE;
