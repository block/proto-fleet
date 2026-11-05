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
-- Replace the source's signal state after receive, pending-edge, watchdog,
-- and retry processing. Singleton per source.
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
WITH changed AS (
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
RETURNING *
)
SELECT changed.*
FROM changed
CROSS JOIN LATERAL request_curtailment_rig_config_reconciliation(
    changed.organization_id, changed.service_user_id, TRUE
) requested;

-- name: UpdateMQTTSourceConfig :one
WITH changed AS (
UPDATE curtailment_mqtt_source_config config
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
WHERE config.id = sqlc.arg('id')
  AND config.organization_id = sqlc.arg('organization_id')
RETURNING *
)
SELECT changed.*
FROM changed
CROSS JOIN LATERAL request_curtailment_rig_config_reconciliation(
    changed.organization_id, changed.service_user_id, TRUE
) requested;

-- name: SetMQTTSourceConfigEnabled :one
WITH changed AS (
UPDATE curtailment_mqtt_source_config config
SET enabled = sqlc.arg('enabled')
WHERE config.id = sqlc.arg('id')
  AND config.organization_id = sqlc.arg('organization_id')
AND (sqlc.arg('enabled') = TRUE OR NOT EXISTS (
 SELECT 1 FROM curtailment_automation_rule rule
 WHERE rule.mqtt_source_id = config.id AND rule.org_id = config.organization_id
 AND curtailment_automation_rule_has_live_event(rule.id, rule.org_id)
))
RETURNING *
)
SELECT changed.*
FROM changed
CROSS JOIN LATERAL request_curtailment_rig_config_reconciliation(
    changed.organization_id, changed.service_user_id, TRUE
) requested;

-- name: DeleteDisabledMQTTSourceConfigByOrg :one
WITH changed AS (
DELETE FROM curtailment_mqtt_source_config config
WHERE config.id = sqlc.arg('id')
  AND config.organization_id = sqlc.arg('organization_id')
  AND config.enabled = FALSE
RETURNING config.organization_id, config.service_user_id
)
SELECT COUNT(*)::BIGINT
FROM changed
CROSS JOIN LATERAL request_curtailment_rig_config_reconciliation(
    changed.organization_id, changed.service_user_id, TRUE
) requested;

-- name: RequestRigConfigReconciliationForDevices :exec
-- Lock/update the organization before writing targets. Completion and terminal
-- retry follow the same lock order so concurrent requests cannot lose targets.
WITH eligible AS MATERIALIZED (
    SELECT d.id, d.org_id
    FROM device d
    JOIN discovered_device dd ON dd.id = d.discovered_device_id
    JOIN device_pairing dp ON dp.device_id = d.id
    WHERE d.org_id = sqlc.arg('organization_id')
      AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
      AND d.deleted_at IS NULL
      AND dd.manufacturer = 'Proto'
      AND dd.device_class <> sqlc.arg('container_device_class')::integer
      AND dp.pairing_status = 'PAIRED'
), reset_budget AS (
 DELETE FROM curtailment_rig_config_delivery_attempt attempt USING eligible
 WHERE attempt.organization_id = eligible.org_id AND attempt.device_id = eligible.id RETURNING attempt.device_id
), requested AS (
    SELECT organizations.org_id AS organization_id,
           request_curtailment_rig_config_reconciliation(
               organizations.org_id, sqlc.arg('requested_by')::bigint, FALSE
           ) AS desired_generation
    FROM (SELECT DISTINCT org_id FROM eligible) organizations
)
INSERT INTO curtailment_rig_config_target (
    organization_id,
    device_id,
    requested_generation
)
SELECT requested.organization_id, eligible.id, requested.desired_generation
FROM requested
JOIN eligible ON eligible.org_id = requested.organization_id
ORDER BY eligible.id
ON CONFLICT (organization_id, device_id) DO UPDATE
SET requested_generation = EXCLUDED.requested_generation;

-- name: ClaimRigConfigReconciliation :one
WITH candidate AS (
    SELECT reconciliation.organization_id
    FROM curtailment_rig_config_reconciliation AS reconciliation
    WHERE reconciliation.desired_generation > reconciliation.enqueued_generation
      AND reconciliation.retry_at <= CURRENT_TIMESTAMP
      AND (reconciliation.lease_expires_at IS NULL OR reconciliation.lease_expires_at <= CURRENT_TIMESTAMP)
      AND NOT EXISTS (
          -- A second active row means the current and retry batch slots are full.
          SELECT 1
          FROM command_batch_log AS active_batch
          WHERE active_batch.organization_id = reconciliation.organization_id
            AND active_batch.type = 'ApplyCurtailmentConfig'
            AND active_batch.status IN ('PENDING', 'PROCESSING')
            AND EXISTS (
                SELECT 1
                FROM queue_message AS message
                WHERE message.command_batch_log_uuid = active_batch.uuid
            )
          OFFSET 1
      )
    ORDER BY reconciliation.retry_at, reconciliation.organization_id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE curtailment_rig_config_reconciliation reconciliation
SET lease_expires_at = CURRENT_TIMESTAMP + INTERVAL '90 seconds', delivery_attempts = delivery_attempts + 1
FROM candidate
WHERE reconciliation.organization_id = candidate.organization_id
RETURNING reconciliation.*;

-- name: ListRigConfigReconciliationTargets :many
-- Run after claiming in a separate statement: its fresh snapshot includes
-- targets committed by a concurrent requester before the claim obtained its
-- organization lock. Targets refreshed after the claim remain for the next pass.
SELECT d.device_identifier
FROM curtailment_rig_config_target target
JOIN device d ON d.id = target.device_id AND d.org_id = target.organization_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
JOIN device_pairing dp ON dp.device_id = d.id
WHERE target.organization_id = sqlc.arg('organization_id')
  AND target.requested_generation > sqlc.arg('enqueued_generation')
  AND target.requested_generation <= sqlc.arg('desired_generation')
  AND d.deleted_at IS NULL
  AND dd.manufacturer = 'Proto'
  AND dd.device_class <> sqlc.arg('container_device_class')::integer
  AND dp.pairing_status = 'PAIRED'
ORDER BY d.device_identifier;

-- name: CompleteRigConfigReconciliation :exec
WITH completed AS (
UPDATE curtailment_rig_config_reconciliation reconciliation
SET enqueued_generation = GREATEST(reconciliation.enqueued_generation, sqlc.arg('enqueued_generation')),
    retry_at = CASE
        WHEN reconciliation.desired_generation > sqlc.arg('enqueued_generation') THEN CURRENT_TIMESTAMP
        ELSE reconciliation.retry_at
    END,
    lease_expires_at = NULL,
    last_error = NULL,
    delivery_attempts = 0
WHERE reconciliation.organization_id = sqlc.arg('organization_id')
  AND reconciliation.enqueued_generation < sqlc.arg('enqueued_generation')
RETURNING reconciliation.organization_id
)
DELETE FROM curtailment_rig_config_target target
USING completed
WHERE target.organization_id = completed.organization_id
  AND target.requested_generation <= sqlc.arg('enqueued_generation');

-- name: RetryRigConfigReconciliation :exec
-- Each claim counts a delivery attempt. Fast retries back off linearly; once
-- they are spent the organization keeps retrying on a slow period, so a short
-- outage cannot strand stale fallback providers on its rigs.
UPDATE curtailment_rig_config_reconciliation
SET retry_at = CURRENT_TIMESTAMP + CASE
        WHEN delivery_attempts >= sqlc.arg('fast_attempts')::INTEGER
            THEN make_interval(secs => sqlc.arg('slow_retry_seconds')::INTEGER)
        ELSE make_interval(secs => 5 * GREATEST(delivery_attempts, 1))
    END,
    lease_expires_at = NULL,
    last_error = sqlc.arg('last_error')
WHERE organization_id = sqlc.arg('organization_id')
  AND desired_generation >= sqlc.arg('desired_generation');

-- name: RequeueRigConfigReconciliationAfterTerminalFailure :exec
-- The command queue has bounded per-message retries. Retain only the failed
-- eligible device for another attempt; successful devices need no new command.
WITH eligible AS MATERIALIZED (
    SELECT d.id, d.org_id
    FROM device d
    JOIN discovered_device dd ON dd.id = d.discovered_device_id
    JOIN device_pairing dp ON dp.device_id = d.id
    WHERE d.org_id = sqlc.arg('organization_id')
      AND d.id = sqlc.arg('device_id')
      AND d.deleted_at IS NULL
      AND dd.manufacturer = 'Proto'
      AND dd.device_class <> sqlc.arg('container_device_class')::integer
      AND dp.pairing_status = 'PAIRED'
), budget AS (
 -- The first ten terminal failures retry after five seconds; later ones keep
 -- retrying on the ten-minute slow period, so a rig that was unreachable for
 -- longer still receives its current config once it reconnects.
 INSERT INTO curtailment_rig_config_delivery_attempt AS attempt (organization_id, device_id, attempts)
 SELECT org_id,id,1 FROM eligible
 ON CONFLICT (organization_id,device_id) DO UPDATE SET attempts = attempt.attempts + 1
 RETURNING organization_id,device_id,attempts
), requested AS (
UPDATE curtailment_rig_config_reconciliation reconciliation
SET desired_generation = reconciliation.desired_generation + 1,
    retry_at = CASE
        WHEN budget.attempts <= 10 THEN CURRENT_TIMESTAMP + INTERVAL '5 seconds'
        -- A slow retry never postpones other devices' work that is already due.
        WHEN reconciliation.desired_generation > reconciliation.enqueued_generation
            THEN LEAST(reconciliation.retry_at, CURRENT_TIMESTAMP + INTERVAL '10 minutes')
        ELSE CURRENT_TIMESTAMP + INTERVAL '10 minutes'
    END,
    last_error = 'config command reached terminal failure',
    delivery_attempts = 0
FROM eligible JOIN budget ON budget.organization_id = eligible.org_id AND budget.device_id = eligible.id
WHERE reconciliation.organization_id = eligible.org_id
RETURNING reconciliation.organization_id, reconciliation.desired_generation
)
INSERT INTO curtailment_rig_config_target (
    organization_id,
    device_id,
    requested_generation
)
SELECT requested.organization_id, eligible.id, requested.desired_generation
FROM requested
JOIN eligible ON eligible.org_id = requested.organization_id
ON CONFLICT (organization_id, device_id) DO UPDATE
SET requested_generation = EXCLUDED.requested_generation;

-- name: CountLiveCurtailmentEventsByMQTTSource :one
SELECT count(*) FROM curtailment_automation_rule r
WHERE r.org_id = sqlc.arg('organization_id') AND r.mqtt_source_id = sqlc.arg('source_id')
AND curtailment_automation_rule_has_live_event(r.id,r.org_id);

-- name: ListRigConfigSourceScopes :many
-- Every eligible requested rig receives only source providers whose enabled
-- rules select it. Empty source_ids actively clears an obsolete fallback.
SELECT d.device_identifier,
 COALESCE(array_agg(DISTINCT rule.mqtt_source_id) FILTER (WHERE rule.id IS NOT NULL), ARRAY[]::bigint[])::bigint[] AS source_ids
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN (curtailment_automation_rule rule
 JOIN curtailment_response_profile profile ON profile.id = rule.response_profile_id AND profile.org_id = rule.org_id
 JOIN curtailment_response_profile_revision revision ON revision.response_profile_id = profile.id
 JOIN curtailment_automation_execution_grant grant_row ON grant_row.rule_id = rule.id AND grant_row.org_id = rule.org_id AND grant_row.response_profile_id = profile.id AND grant_row.response_profile_revision = revision.revision
 JOIN curtailment_mqtt_source_config src ON src.id = rule.mqtt_source_id AND src.organization_id = rule.org_id AND src.enabled)
ON rule.org_id = d.org_id AND rule.enabled
AND ((grant_row.authorization_envelope_json->>'miner_scope_unbounded')::boolean = TRUE
 OR (grant_row.authorization_envelope_json->'selected_resource_site_ids') @> jsonb_build_array(d.site_id)
 OR (grant_row.authorization_envelope_json->'current_member_site_ids') @> jsonb_build_array(d.site_id))
AND (
 (profile.scope_json->>'whole_org')::boolean = TRUE
 OR (profile.scope_json = '{}'::jsonb AND profile.site_id IS NULL)
 OR profile.site_id = d.site_id
 OR (profile.scope_json->>'site_id')::bigint = d.site_id
 OR (profile.scope_json->'site_ids') @> jsonb_build_array(d.site_id)
 OR (profile.scope_json->'device_identifiers') @> jsonb_build_array(d.device_identifier)
 OR EXISTS (
  SELECT 1 FROM building b WHERE b.org_id = d.org_id AND b.deleted_at IS NULL
  AND (profile.scope_json->'building_ids') @> jsonb_build_array(b.id)
  AND (d.building_id = b.id OR EXISTS (
   SELECT 1 FROM device_set_membership membership JOIN device_set rack ON rack.id = membership.device_set_id AND rack.org_id = membership.org_id AND rack.type = 'rack' AND rack.deleted_at IS NULL
   JOIN device_set_rack placement ON placement.device_set_id = rack.id AND placement.org_id = rack.org_id
   WHERE membership.device_id = d.id AND membership.org_id = d.org_id AND membership.device_set_type = 'rack' AND placement.building_id = b.id
  ))
 )
 OR EXISTS (
  SELECT 1 FROM device_set_membership membership JOIN device_set selected ON selected.id = membership.device_set_id AND selected.org_id = membership.org_id AND selected.deleted_at IS NULL
  WHERE membership.device_id = d.id AND membership.org_id = d.org_id
  AND ((membership.device_set_type = 'rack' AND selected.type = 'rack' AND (profile.scope_json->'rack_ids') @> jsonb_build_array(selected.id))
   OR (membership.device_set_type = 'group' AND selected.type = 'group' AND (profile.scope_json->'group_ids') @> jsonb_build_array(selected.id)))
 )
)
WHERE d.org_id = sqlc.arg('organization_id') AND d.deleted_at IS NULL
AND dd.manufacturer = 'Proto' AND dd.device_class <> sqlc.arg('container_device_class')::integer AND dp.pairing_status = 'PAIRED'
AND (cardinality(sqlc.arg('device_identifiers')::text[]) = 0 OR d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[]))
GROUP BY d.device_identifier ORDER BY d.device_identifier;
