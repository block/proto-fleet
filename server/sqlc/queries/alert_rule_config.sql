-- name: UpsertAlertRuleConfig :exec
INSERT INTO alert_rule_config (org_id, rule_uid, config)
VALUES (sqlc.arg('org_id'), sqlc.arg('rule_uid'), sqlc.arg('config'))
ON CONFLICT (org_id, rule_uid)
DO UPDATE SET config = EXCLUDED.config, updated_at = now();

-- name: GetAlertRuleConfig :one
SELECT config
FROM alert_rule_config
WHERE org_id = sqlc.arg('org_id')
  AND rule_uid = sqlc.arg('rule_uid');

-- Bounded to the caller's rule UIDs so orphan rows (see SweepAlertRuleConfigs)
-- never inflate the list path.
-- name: ListAlertRuleConfigs :many
SELECT rule_uid, config
FROM alert_rule_config
WHERE org_id = sqlc.arg('org_id')
  AND rule_uid = ANY(sqlc.arg('rule_uids')::text[]);

-- Reclaims rows for never-created rule UIDs left by ambiguous create failures (see CreateRule).
-- The hour of slack protects in-flight creates, whose config row lands before the Grafana rule exists.
-- name: SweepAlertRuleConfigs :execrows
DELETE FROM alert_rule_config
WHERE org_id = sqlc.arg('org_id')
  AND updated_at < now() - INTERVAL '1 hour'
  AND NOT (rule_uid = ANY(sqlc.arg('live_rule_uids')::text[]));

-- name: DeleteAlertRuleConfig :exec
DELETE FROM alert_rule_config
WHERE org_id = sqlc.arg('org_id')
  AND rule_uid = sqlc.arg('rule_uid');
