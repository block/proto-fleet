-- name: ListEnabledMQTTSources :many
-- Enabled MQTT sources, read once at subscriber startup. Enable/disable
-- takes effect on the next start (no hot reload).
SELECT *
FROM curtailment_mqtt_source_config
WHERE enabled = TRUE
ORDER BY id;

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
    last_processed_target,
    last_processed_targets,
    last_received_at,
    last_received_broker,
    last_edge_at,
    last_edge_event_uuid,
    pending_direction,
    pending_target,
    pending_target_at,
    pending_received_at,
    pending_received_broker,
    pending_prior_edge_at,
    last_empty_full_fleet_watchdog_ref
) VALUES (
    sqlc.arg('source_config_id'),
    sqlc.narg('last_target'),
    sqlc.narg('last_target_at'),
    sqlc.narg('last_processed_target'),
    sqlc.narg('last_processed_targets'),
    sqlc.narg('last_received_at'),
    sqlc.narg('last_received_broker'),
    sqlc.narg('last_edge_at'),
    sqlc.narg('last_edge_event_uuid'),
    sqlc.narg('pending_direction'),
    sqlc.narg('pending_target'),
    sqlc.narg('pending_target_at'),
    sqlc.narg('pending_received_at'),
    sqlc.narg('pending_received_broker'),
    sqlc.narg('pending_prior_edge_at'),
    sqlc.narg('last_empty_full_fleet_watchdog_ref')
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
    last_edge_event_uuid   = EXCLUDED.last_edge_event_uuid,
    pending_direction      = EXCLUDED.pending_direction,
    pending_target         = EXCLUDED.pending_target,
    pending_target_at      = EXCLUDED.pending_target_at,
    pending_received_at    = EXCLUDED.pending_received_at,
    pending_received_broker = EXCLUDED.pending_received_broker,
    pending_prior_edge_at  = EXCLUDED.pending_prior_edge_at,
    last_empty_full_fleet_watchdog_ref = EXCLUDED.last_empty_full_fleet_watchdog_ref;

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
    broker_transport,
    mqtt_username,
    mqtt_password_enc,
    contracted_curtailment_kw,
    curtail_mode,
    payload_format,
    scope_type,
    scope_site_id,
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
    sqlc.arg('broker_transport'),
    sqlc.arg('mqtt_username'),
    sqlc.arg('mqtt_password_enc'),
    sqlc.narg('contracted_curtailment_kw'),
    sqlc.arg('curtail_mode'),
    sqlc.arg('payload_format'),
    sqlc.arg('scope_type'),
    sqlc.narg('scope_site_id'),
    sqlc.narg('scope_device_identifiers'),
    sqlc.narg('staleness_threshold_sec'),
    sqlc.narg('min_curtailed_duration_sec'),
    sqlc.arg('enabled')
)
RETURNING *;
