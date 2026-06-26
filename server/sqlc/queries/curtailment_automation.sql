-- name: ListCurtailmentAutomationRulesByOrg :many
SELECT
    r.*,
    src.source_name AS mqtt_source_name,
    st.last_signal,
    st.last_signal_at,
    st.active_event_uuid,
    st.last_started_at,
    st.last_restored_at,
    st.last_error,
    st.last_error_at,
    profile.profile_name AS response_profile_name,
    profile.site_id AS response_profile_site_id,
    profile.scope_json AS response_profile_scope_json
FROM curtailment_automation_rule r
JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
    AND src.organization_id = r.org_id
JOIN curtailment_response_profile profile
    ON profile.id = r.response_profile_id
    AND profile.org_id = r.org_id
LEFT JOIN curtailment_automation_rule_state st
    ON st.rule_id = r.id
WHERE r.org_id = sqlc.arg('org_id')
ORDER BY r.id;

-- name: GetCurtailmentAutomationRuleByOrg :one
SELECT
    r.*,
    src.source_name AS mqtt_source_name,
    st.last_signal,
    st.last_signal_at,
    st.active_event_uuid,
    st.last_started_at,
    st.last_restored_at,
    st.last_error,
    st.last_error_at,
    profile.profile_name AS response_profile_name,
    profile.site_id AS response_profile_site_id,
    profile.scope_json AS response_profile_scope_json
FROM curtailment_automation_rule r
JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
    AND src.organization_id = r.org_id
JOIN curtailment_response_profile profile
    ON profile.id = r.response_profile_id
    AND profile.org_id = r.org_id
LEFT JOIN curtailment_automation_rule_state st
    ON st.rule_id = r.id
WHERE r.id = sqlc.arg('id')
  AND r.org_id = sqlc.arg('org_id');

-- name: ListEnabledCurtailmentAutomationRulesByMQTTSource :many
SELECT
    r.*,
    src.source_name AS mqtt_source_name,
    st.last_signal,
    st.last_signal_at,
    st.active_event_uuid,
    st.last_started_at,
    st.last_restored_at,
    st.last_error,
    st.last_error_at,
    profile.profile_name AS response_profile_name,
    profile.site_id AS response_profile_site_id,
    profile.scope_json AS response_profile_scope_json
FROM curtailment_automation_rule r
JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
    AND src.organization_id = r.org_id
JOIN curtailment_response_profile profile
    ON profile.id = r.response_profile_id
    AND profile.org_id = r.org_id
LEFT JOIN curtailment_automation_rule_state st
    ON st.rule_id = r.id
WHERE r.mqtt_source_id = sqlc.arg('mqtt_source_id')
  AND r.enabled = TRUE
ORDER BY r.id;

-- name: GetEnabledCurtailmentAutomationRuleByEvent :one
SELECT
    r.*,
    src.source_name AS mqtt_source_name,
    st.last_signal,
    st.last_signal_at,
    st.active_event_uuid,
    st.last_started_at,
    st.last_restored_at,
    st.last_error,
    st.last_error_at,
    profile.profile_name AS response_profile_name,
    profile.site_id AS response_profile_site_id,
    profile.scope_json AS response_profile_scope_json
FROM curtailment_automation_rule r
JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
    AND src.organization_id = r.org_id
JOIN curtailment_response_profile profile
    ON profile.id = r.response_profile_id
    AND profile.org_id = r.org_id
JOIN curtailment_automation_rule_state st
    ON st.rule_id = r.id
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
  AND NOT EXISTS (
      SELECT 1
      FROM curtailment_automation_rule_state st
      JOIN curtailment_event e
          ON e.event_uuid = st.active_event_uuid
      WHERE st.rule_id = curtailment_automation_rule.id
        AND e.state IN ('pending', 'active', 'restoring')
  )
RETURNING *;

-- name: SetCurtailmentAutomationRuleEnabled :one
UPDATE curtailment_automation_rule
SET enabled = sqlc.arg('enabled')
WHERE curtailment_automation_rule.id = sqlc.arg('id')
  AND curtailment_automation_rule.org_id = sqlc.arg('org_id')
  AND (
      sqlc.arg('enabled') = TRUE
      OR NOT EXISTS (
          SELECT 1
          FROM curtailment_automation_rule_state st
          JOIN curtailment_event e
              ON e.event_uuid = st.active_event_uuid
          WHERE st.rule_id = curtailment_automation_rule.id
            AND e.state IN ('pending', 'active', 'restoring')
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
  AND NOT EXISTS (
      SELECT 1
      FROM curtailment_automation_rule_state st
      JOIN curtailment_event e
          ON e.event_uuid = st.active_event_uuid
      WHERE st.rule_id = curtailment_automation_rule.id
        AND e.state IN ('pending', 'active', 'restoring')
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
    SELECT id
    FROM curtailment_automation_rule
    WHERE id = sqlc.arg('rule_id')
      AND enabled = TRUE
    FOR UPDATE
)
INSERT INTO curtailment_automation_rule_state (
    rule_id,
    active_event_uuid,
    last_started_at,
    last_error,
    last_error_at
)
SELECT
    id,
    sqlc.arg('active_event_uuid'),
    sqlc.arg('last_started_at'),
    NULL,
    NULL
FROM enabled_rule
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
    last_restored_at = EXCLUDED.last_restored_at,
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
