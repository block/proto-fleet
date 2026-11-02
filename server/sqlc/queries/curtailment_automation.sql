-- name: ListCurtailmentAutomationRulesByOrg :many
SELECT r.*, st.last_signal, st.last_signal_at, st.active_event_uuid,
       st.last_started_at, st.last_restored_at, st.last_error, st.last_error_at,
       CASE WHEN NOT r.enabled THEN 'disabled'
            WHEN EXISTS (SELECT 1 FROM curtailment_automation_execution_grant g
                 JOIN curtailment_response_profile_with_revision p ON p.id = g.response_profile_id AND p.org_id = g.org_id
                 WHERE g.rule_id = r.id AND g.response_profile_revision = r.response_profile_revision AND p.revision = r.response_profile_revision)
            THEN 'bound' ELSE 'rebind_required' END::text AS binding_status,
       COALESCE((SELECT ex.status FROM curtailment_automation_execution ex WHERE ex.rule_id = r.id), 'idle')::text AS execution_status
FROM curtailment_automation_rule_binding r
LEFT JOIN curtailment_automation_rule_state st ON st.rule_id = r.id
WHERE r.org_id = sqlc.arg('org_id')
ORDER BY r.id;

-- name: GetCurtailmentAutomationRuleByOrg :one
SELECT r.*, st.last_signal, st.last_signal_at, st.active_event_uuid,
       st.last_started_at, st.last_restored_at, st.last_error, st.last_error_at,
       CASE WHEN NOT r.enabled THEN 'disabled'
            WHEN EXISTS (SELECT 1 FROM curtailment_automation_execution_grant g
                 JOIN curtailment_response_profile_with_revision p ON p.id = g.response_profile_id AND p.org_id = g.org_id
                 WHERE g.rule_id = r.id AND g.response_profile_revision = r.response_profile_revision AND p.revision = r.response_profile_revision)
            THEN 'bound' ELSE 'rebind_required' END::text AS binding_status,
       COALESCE((SELECT ex.status FROM curtailment_automation_execution ex WHERE ex.rule_id = r.id), 'idle')::text AS execution_status
FROM curtailment_automation_rule_binding r
LEFT JOIN curtailment_automation_rule_state st ON st.rule_id = r.id
WHERE r.id = sqlc.arg('id')
  AND r.org_id = sqlc.arg('org_id');

-- name: LockCurtailmentAutomationRuleMutation :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(
        'curtailment_automation_rule:'
            || sqlc.arg('org_id')::bigint::text
            || ':'
            || sqlc.arg('rule_id')::bigint::text,
        0
    )
);

-- name: LockCurtailmentAutomationRuleForExecution :one
SELECT rule.id
FROM curtailment_automation_rule AS rule
JOIN curtailment_automation_rule_profile_revision AS rule_revision
  ON rule_revision.automation_rule_id = rule.id
JOIN curtailment_mqtt_source_config AS source
  ON source.id = rule.mqtt_source_id
 AND source.organization_id = rule.org_id
WHERE rule.id = sqlc.arg('id')
  AND rule.org_id = sqlc.arg('org_id')
  AND rule.enabled = TRUE
  AND rule.trigger_type = 'MQTT'
  AND rule.mqtt_source_id = sqlc.arg('mqtt_source_id')
  AND source.enabled = TRUE
  AND rule.response_profile_id = sqlc.arg('response_profile_id')
  AND rule_revision.response_profile_revision = sqlc.arg('response_profile_revision')
FOR UPDATE OF rule, rule_revision, source;

-- name: ListEnabledCurtailmentAutomationRulesByMQTTSource :many
SELECT r.*, st.last_signal, st.last_signal_at, st.active_event_uuid,
       st.last_started_at, st.last_restored_at, st.last_error, st.last_error_at,
       CASE WHEN NOT r.enabled THEN 'disabled'
            WHEN EXISTS (SELECT 1 FROM curtailment_automation_execution_grant g
                 JOIN curtailment_response_profile_with_revision p ON p.id = g.response_profile_id AND p.org_id = g.org_id
                 WHERE g.rule_id = r.id AND g.response_profile_revision = r.response_profile_revision AND p.revision = r.response_profile_revision)
            THEN 'bound' ELSE 'rebind_required' END::text AS binding_status,
       COALESCE((SELECT ex.status FROM curtailment_automation_execution ex WHERE ex.rule_id = r.id), 'idle')::text AS execution_status
FROM curtailment_automation_rule_binding r
LEFT JOIN curtailment_automation_rule_state st ON st.rule_id = r.id
WHERE r.mqtt_source_id = sqlc.arg('mqtt_source_id')
  AND r.enabled = TRUE
ORDER BY r.id;

-- name: GetEnabledCurtailmentAutomationRuleByEvent :one
SELECT r.*, st.last_signal, st.last_signal_at, st.active_event_uuid,
       st.last_started_at, st.last_restored_at, st.last_error, st.last_error_at,
       CASE WHEN NOT r.enabled THEN 'disabled'
            WHEN EXISTS (SELECT 1 FROM curtailment_automation_execution_grant g
                 JOIN curtailment_response_profile_with_revision p ON p.id = g.response_profile_id AND p.org_id = g.org_id
                 WHERE g.rule_id = r.id AND g.response_profile_revision = r.response_profile_revision AND p.revision = r.response_profile_revision)
            THEN 'bound' ELSE 'rebind_required' END::text AS binding_status,
       COALESCE((SELECT ex.status FROM curtailment_automation_execution ex WHERE ex.rule_id = r.id), 'idle')::text AS execution_status
FROM curtailment_automation_rule_binding r
JOIN curtailment_automation_rule_state st ON st.rule_id = r.id
WHERE r.org_id = sqlc.arg('org_id')
  AND r.enabled = TRUE
  AND (
      st.active_event_uuid = sqlc.arg('event_uuid')
      OR (
          sqlc.narg('external_reference')::text IS NOT NULL
          AND r.id::text = sqlc.narg('external_reference')::text
      )
  )
ORDER BY r.id
LIMIT 1
FOR UPDATE OF st;

-- name: InsertCurtailmentAutomationRule :one
INSERT INTO curtailment_automation_rule (
    org_id,
    rule_name,
    trigger_type,
    mqtt_source_id,
    response_profile_id,
    enabled
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('rule_name'),
    sqlc.arg('trigger_type'),
    sqlc.arg('mqtt_source_id'),
    sqlc.arg('response_profile_id'),
    sqlc.arg('enabled')
)
RETURNING *;

-- name: UpdateCurtailmentAutomationRule :one
UPDATE curtailment_automation_rule
SET
    rule_name = sqlc.arg('rule_name'),
    mqtt_source_id = sqlc.arg('mqtt_source_id'),
    response_profile_id = sqlc.arg('response_profile_id')
WHERE curtailment_automation_rule.id = sqlc.arg('id')
  AND curtailment_automation_rule.org_id = sqlc.arg('org_id')
  AND NOT curtailment_automation_rule_has_live_event(
      curtailment_automation_rule.id, curtailment_automation_rule.org_id
  )
RETURNING *;

-- name: BindCurtailmentAutomationRuleResponseProfileRevision :execrows
UPDATE curtailment_automation_rule_profile_revision AS rule_revision
SET response_profile_revision = sqlc.arg('response_profile_revision')
FROM curtailment_automation_rule AS rule
WHERE rule_revision.automation_rule_id = rule.id
  AND rule.id = sqlc.arg('id')
  AND rule.org_id = sqlc.arg('org_id')
  AND rule.response_profile_id = sqlc.arg('expected_response_profile_id');

-- name: SetCurtailmentAutomationRuleEnabled :one
UPDATE curtailment_automation_rule
SET
    enabled = sqlc.arg('enabled')
WHERE curtailment_automation_rule.id = sqlc.arg('id')
  AND curtailment_automation_rule.org_id = sqlc.arg('org_id')
  AND (
      sqlc.arg('enabled') = FALSE
      OR response_profile_id = sqlc.arg('expected_response_profile_id')
  )
  AND (
      NOT curtailment_automation_rule_has_live_event(
          curtailment_automation_rule.id, curtailment_automation_rule.org_id
      )
      OR (
          sqlc.arg('enabled') = TRUE
          AND curtailment_automation_rule.enabled = TRUE
          AND EXISTS (
              SELECT 1
              FROM curtailment_automation_rule_profile_revision AS rule_revision
              WHERE rule_revision.automation_rule_id = curtailment_automation_rule.id
                AND rule_revision.response_profile_revision = sqlc.arg('response_profile_revision')
          )
      )
  )
RETURNING *;

-- name: DisableCurtailmentAutomationRuleByActiveEvent :execrows
UPDATE curtailment_automation_rule r
SET enabled = FALSE
FROM curtailment_automation_rule_state st
LEFT JOIN curtailment_event active_event
    ON active_event.event_uuid = st.active_event_uuid
    AND active_event.org_id = sqlc.arg('org_id')
WHERE st.rule_id = r.id
  AND r.org_id = sqlc.arg('org_id')
  AND r.enabled = TRUE
  AND (
      st.active_event_uuid = sqlc.arg('event_uuid')
      OR (
          sqlc.narg('external_reference')::text IS NOT NULL
          AND r.id::text = sqlc.narg('external_reference')::text
          AND (
              st.active_event_uuid IS NULL
              OR active_event.state IS NULL
              OR active_event.state NOT IN ('pending', 'active', 'restoring')
          )
      )
  );

-- name: DeleteCurtailmentAutomationRuleByOrg :execrows
DELETE FROM curtailment_automation_rule
WHERE curtailment_automation_rule.id = sqlc.arg('id')
  AND curtailment_automation_rule.org_id = sqlc.arg('org_id')
  AND NOT curtailment_automation_rule_has_live_event(
      curtailment_automation_rule.id, curtailment_automation_rule.org_id
  );

-- name: CountCurtailmentAutomationRulesByMQTTSource :one
SELECT count(*)
FROM curtailment_automation_rule
WHERE org_id = sqlc.arg('org_id')
  AND mqtt_source_id = sqlc.arg('mqtt_source_id');

-- name: CountCurtailmentAutomationRulesByResponseProfile :one
SELECT count(*)
FROM curtailment_automation_rule
WHERE org_id = sqlc.arg('org_id')
  AND response_profile_id = sqlc.arg('response_profile_id');

-- name: UpsertCurtailmentAutomationSignalState :exec
INSERT INTO curtailment_automation_rule_state (
    rule_id,
    last_signal,
    last_signal_at,
    last_error,
    last_error_at
) VALUES (
    sqlc.arg('rule_id'),
    sqlc.arg('last_signal'),
    sqlc.arg('last_signal_at'),
    NULL,
    NULL
)
ON CONFLICT (rule_id) DO UPDATE
SET
    last_signal = EXCLUDED.last_signal,
    last_signal_at = EXCLUDED.last_signal_at,
    last_error = NULL,
    last_error_at = NULL;

-- name: SetCurtailmentAutomationActiveEvent :execrows
WITH enabled_rule AS (
    SELECT curtailment_automation_rule.id, curtailment_automation_rule.org_id
    FROM curtailment_automation_rule
    WHERE curtailment_automation_rule.id = sqlc.arg('rule_id')
      AND enabled = TRUE
      -- The rule must still be bound to the source whose signal started the
      -- event; a re-point between signal read and event start fails here and
      -- the caller force-releases the orphaned event.
      AND mqtt_source_id = sqlc.arg('mqtt_source_id')
    FOR UPDATE
),
owned_event AS (
    SELECT ce.event_uuid
    FROM curtailment_event ce
    JOIN enabled_rule er
        ON er.org_id = ce.org_id
    WHERE ce.event_uuid = sqlc.arg('active_event_uuid')
      AND ce.source_actor_type = 'automation'
      AND ce.source_actor_id = er.id::TEXT
      AND ce.external_source = 'curtailment_automation'
      AND ce.external_reference = er.id::TEXT
      AND ce.idempotency_key = 'curtailment_automation_rule:' || er.id::TEXT
)
INSERT INTO curtailment_automation_rule_state (
    rule_id,
    active_event_uuid,
    last_started_at,
    last_error,
    last_error_at
)
SELECT
    enabled_rule.id,
    owned_event.event_uuid,
    sqlc.arg('last_started_at'),
    NULL,
    NULL
FROM enabled_rule
JOIN owned_event ON true
ON CONFLICT (rule_id) DO UPDATE
SET
    active_event_uuid = EXCLUDED.active_event_uuid,
    last_started_at = EXCLUDED.last_started_at,
    last_error = NULL,
    last_error_at = NULL;

-- name: ClearCurtailmentAutomationActiveEvent :exec
INSERT INTO curtailment_automation_rule_state (
    rule_id,
    active_event_uuid,
    last_restored_at,
    last_error,
    last_error_at
) VALUES (
    sqlc.arg('rule_id'),
    NULL,
    sqlc.arg('last_restored_at'),
    NULL,
    NULL
)
ON CONFLICT (rule_id) DO UPDATE
SET
    active_event_uuid = NULL,
    last_restored_at = COALESCE(EXCLUDED.last_restored_at, curtailment_automation_rule_state.last_restored_at),
    last_error = NULL,
    last_error_at = NULL;

-- name: SetCurtailmentAutomationRestoreStarted :exec
INSERT INTO curtailment_automation_rule_state (
    rule_id,
    last_restored_at,
    last_error,
    last_error_at
) VALUES (
    sqlc.arg('rule_id'),
    sqlc.arg('last_restored_at'),
    NULL,
    NULL
)
ON CONFLICT (rule_id) DO UPDATE
SET
    last_restored_at = EXCLUDED.last_restored_at,
    last_error = NULL,
    last_error_at = NULL;

-- name: SetCurtailmentAutomationExecutionError :exec
INSERT INTO curtailment_automation_rule_state (
    rule_id,
    last_error,
    last_error_at
) VALUES (
    sqlc.arg('rule_id'),
    sqlc.arg('last_error'),
    sqlc.arg('last_error_at')
)
ON CONFLICT (rule_id) DO UPDATE
SET
    last_error = EXCLUDED.last_error,
    last_error_at = EXCLUDED.last_error_at;

-- name: ListMQTTSourcesWithActiveCurtailment :many
-- Sources (enabled or not) whose automation started a curtailment event that
-- is still non-terminal. Matched via the event's external reference rather
-- than rule state, so a source or rule disabled after the event started, or
-- a crash before the active-event pointer was written, cannot hide it.
SELECT DISTINCT
    src.id AS source_id,
    src.organization_id,
    src.source_name
FROM curtailment_event e
JOIN curtailment_automation_rule r
    ON r.org_id = e.org_id
    AND r.id::text = e.external_reference
JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
    AND src.organization_id = r.org_id
WHERE e.external_source = 'curtailment_automation'
  AND e.state IN ('pending', 'active', 'restoring');

-- name: CountEnabledCurtailmentAutomationRulesByResponseProfile :one
SELECT count(*) FROM curtailment_automation_rule
WHERE org_id = sqlc.arg('org_id') AND response_profile_id = sqlc.arg('response_profile_id') AND enabled;

-- name: UpsertCurtailmentAutomationExecutionGrant :exec
INSERT INTO curtailment_automation_execution_grant (rule_id, org_id, response_profile_id, response_profile_revision, authorization_envelope_json, admin_controls_granted)
SELECT r.id, r.org_id, p.id, p.revision, p.authorization_envelope_jsonb, sqlc.arg('admin_controls_granted')
FROM curtailment_automation_rule r
JOIN curtailment_response_profile_with_revision p ON p.id = r.response_profile_id AND p.org_id = r.org_id
WHERE r.id = sqlc.arg('rule_id') AND r.org_id = sqlc.arg('org_id')
ON CONFLICT (rule_id) DO UPDATE SET response_profile_id = EXCLUDED.response_profile_id,
 response_profile_revision = EXCLUDED.response_profile_revision, authorization_envelope_json = EXCLUDED.authorization_envelope_json,
 admin_controls_granted = EXCLUDED.admin_controls_granted;

-- name: GetCurtailmentAutomationExecutionGrant :one
SELECT grant_row.*
FROM curtailment_automation_execution_grant grant_row
JOIN curtailment_automation_rule r ON r.id = grant_row.rule_id AND r.org_id = grant_row.org_id
JOIN curtailment_automation_rule_profile_revision revision ON revision.automation_rule_id = r.id
JOIN curtailment_response_profile_with_revision profile ON profile.id = r.response_profile_id AND profile.org_id = r.org_id
JOIN curtailment_mqtt_source_config source ON source.id = r.mqtt_source_id AND source.organization_id = r.org_id
WHERE r.id = sqlc.arg('rule_id') AND r.org_id = sqlc.arg('org_id') AND r.enabled AND source.enabled
AND grant_row.response_profile_id = r.response_profile_id AND grant_row.response_profile_revision = revision.response_profile_revision
AND grant_row.response_profile_revision = profile.revision
FOR SHARE OF grant_row, r, revision, source;

-- name: QueueCurtailmentAutomationExecution :exec
INSERT INTO curtailment_automation_execution (rule_id, mqtt_source_id, direction, target, target_at, received_at, received_broker, prior_edge_at)
SELECT r.id, r.mqtt_source_id, sqlc.arg('direction'), sqlc.arg('target'), sqlc.narg('target_at'), sqlc.arg('received_at'), sqlc.arg('received_broker'), sqlc.narg('prior_edge_at')
FROM curtailment_automation_rule r WHERE r.id = sqlc.arg('rule_id') AND r.enabled AND r.mqtt_source_id = sqlc.arg('mqtt_source_id')
ON CONFLICT (rule_id) DO UPDATE SET generation = curtailment_automation_execution.generation + 1,
 mqtt_source_id = EXCLUDED.mqtt_source_id, direction = EXCLUDED.direction, target = EXCLUDED.target,
 target_at = EXCLUDED.target_at, received_at = EXCLUDED.received_at, received_broker = EXCLUDED.received_broker, prior_edge_at = EXCLUDED.prior_edge_at,
 status = 'pending', attempts = 0, retry_at = now(), last_error = NULL
WHERE (
 curtailment_automation_execution.received_at < EXCLUDED.received_at
 OR (curtailment_automation_execution.received_at = EXCLUDED.received_at AND curtailment_automation_execution.target <> EXCLUDED.target)
 OR curtailment_automation_execution.mqtt_source_id <> EXCLUDED.mqtt_source_id
) AND NOT (
 curtailment_automation_execution.mqtt_source_id = EXCLUDED.mqtt_source_id
 AND curtailment_automation_execution.target = EXCLUDED.target
 AND curtailment_automation_execution.status IN ('pending','retry')
 AND EXCLUDED.direction = 'reassert_off'
);

-- name: QueueCurrentCurtailmentAutomationExecution :exec
INSERT INTO curtailment_automation_execution (rule_id, mqtt_source_id, direction, target, target_at, received_at, received_broker, prior_edge_at)
SELECT r.id, r.mqtt_source_id,
 CASE COALESCE(st.pending_target, st.last_target) WHEN 'OFF' THEN 'reassert_off' ELSE 'off_to_on' END,
 COALESCE(st.pending_target, st.last_target), COALESCE(st.pending_target_at, st.last_target_at), now(), COALESCE(st.last_received_broker,''), st.last_edge_at
FROM curtailment_automation_rule r JOIN curtailment_mqtt_source_state st ON st.source_config_id = r.mqtt_source_id
JOIN curtailment_mqtt_source_config src ON src.id = r.mqtt_source_id
WHERE r.id = sqlc.arg('rule_id') AND r.enabled AND src.enabled AND COALESCE(st.pending_target, st.last_target) IN ('ON','OFF')
ON CONFLICT (rule_id) DO UPDATE SET generation = curtailment_automation_execution.generation + 1,
 mqtt_source_id = EXCLUDED.mqtt_source_id, direction = EXCLUDED.direction, target = EXCLUDED.target, target_at = EXCLUDED.target_at,
 received_at = EXCLUDED.received_at, received_broker = EXCLUDED.received_broker, prior_edge_at = EXCLUDED.prior_edge_at,
 status = 'pending', attempts = 0, retry_at = now(), last_error = NULL;

-- name: ClaimCurtailmentAutomationExecution :one
WITH due AS (
 SELECT ex.rule_id, r.org_id FROM curtailment_automation_execution ex
 JOIN curtailment_automation_rule r ON r.id = ex.rule_id AND r.enabled
 JOIN curtailment_mqtt_source_config src ON src.id = ex.mqtt_source_id AND src.enabled
 WHERE ex.status IN ('pending','retry') AND ex.retry_at <= now() AND (ex.lease_expires_at IS NULL OR ex.lease_expires_at <= now())
 ORDER BY ex.retry_at, ex.rule_id FOR UPDATE OF ex SKIP LOCKED LIMIT 1
)
UPDATE curtailment_automation_execution ex SET lease_expires_at = now() + interval '90 seconds', attempts = ex.attempts + 1
FROM due WHERE due.rule_id = ex.rule_id RETURNING ex.*, due.org_id;

-- name: CompleteCurtailmentAutomationExecution :exec
UPDATE curtailment_automation_execution SET status = CASE WHEN generation = sqlc.arg('generation') THEN 'completed' ELSE status END, lease_expires_at = NULL, last_error = CASE WHEN generation = sqlc.arg('generation') THEN NULL ELSE last_error END
WHERE rule_id = sqlc.arg('rule_id') AND lease_expires_at = sqlc.arg('lease_expires_at')::timestamptz;

-- name: RetryCurtailmentAutomationExecution :exec
UPDATE curtailment_automation_execution SET status = CASE WHEN generation = sqlc.arg('generation') THEN 'retry' ELSE status END, lease_expires_at = NULL,
 retry_at = CASE WHEN generation = sqlc.arg('generation') THEN now() + LEAST(300, 5 * attempts) * interval '1 second' ELSE retry_at END, last_error = CASE WHEN generation = sqlc.arg('generation') THEN sqlc.narg('last_error') ELSE last_error END
WHERE rule_id = sqlc.arg('rule_id') AND lease_expires_at = sqlc.arg('lease_expires_at')::timestamptz;
