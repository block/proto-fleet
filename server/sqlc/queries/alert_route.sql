-- name: UpsertAlertRoutePolicy :one
INSERT INTO alert_route_policy (org_id, rule_uid, mode)
VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('rule_uid'),
    sqlc.arg('mode')
)
ON CONFLICT (org_id, rule_uid)
DO UPDATE SET mode = EXCLUDED.mode, updated_at = now()
RETURNING *;

-- name: DeleteAlertRoutePolicy :execrows
DELETE FROM alert_route_policy
WHERE org_id = sqlc.arg('org_id')
  AND rule_uid = sqlc.arg('rule_uid');

-- name: DeleteAlertRouteChannels :exec
DELETE FROM alert_route_channel
WHERE policy_id = sqlc.arg('policy_id');

-- name: InsertAlertRouteChannels :exec
-- DO NOTHING tolerates duplicate ids in one call rather than aborting the surrounding SetPolicy transaction.
INSERT INTO alert_route_channel (policy_id, channel_id)
SELECT sqlc.arg('policy_id'), unnest(sqlc.arg('channel_ids')::bigint[])
ON CONFLICT DO NOTHING;

-- name: ListAlertRoutePolicies :many
-- channel_ids counts only the org's live channels, so a soft-deleted channel drops out of every policy that referenced it.
SELECT
    p.rule_uid,
    p.mode,
    COALESCE(array_agg(c.id ORDER BY c.id) FILTER (WHERE c.id IS NOT NULL), '{}')::bigint[] AS channel_ids
FROM alert_route_policy p
LEFT JOIN alert_route_channel rc ON rc.policy_id = p.id
LEFT JOIN alert_channel c ON c.id = rc.channel_id AND c.org_id = p.org_id AND c.deleted_at IS NULL
WHERE p.org_id = sqlc.arg('org_id')
GROUP BY p.id
ORDER BY p.rule_uid;
