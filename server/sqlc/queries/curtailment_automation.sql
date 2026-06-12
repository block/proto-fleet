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
    profile.profile_name AS response_profile_name
FROM curtailment_automation_rule r
JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
JOIN curtailment_response_profile profile
    ON profile.id = r.response_profile_id
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
    profile.profile_name AS response_profile_name
FROM curtailment_automation_rule r
JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
JOIN curtailment_response_profile profile
    ON profile.id = r.response_profile_id
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
    profile.profile_name AS response_profile_name
FROM curtailment_automation_rule r
JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
JOIN curtailment_response_profile profile
    ON profile.id = r.response_profile_id
LEFT JOIN curtailment_automation_rule_state st
    ON st.rule_id = r.id
WHERE r.mqtt_source_id = sqlc.arg('mqtt_source_id')
  AND r.enabled = TRUE
ORDER BY r.id;

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
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
RETURNING *;

-- name: SetCurtailmentAutomationRuleEnabled :one
UPDATE curtailment_automation_rule
SET enabled = sqlc.arg('enabled')
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
RETURNING *;

-- name: DeleteCurtailmentAutomationRuleByOrg :execrows
DELETE FROM curtailment_automation_rule
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id');

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

-- name: SetCurtailmentAutomationActiveEvent :exec
INSERT INTO curtailment_automation_rule_state (
    rule_id,
    active_event_uuid,
    last_started_at,
    last_error,
    last_error_at
) VALUES (
    sqlc.arg('rule_id'),
    sqlc.arg('active_event_uuid'),
    sqlc.arg('last_started_at'),
    NULL,
    NULL
)
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
