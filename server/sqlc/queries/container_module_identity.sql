-- name: GetContainerModuleIdentities :many
SELECT i.reported_module_id,i.module_id,m.last_seen_at,m.retired_at,d.device_identifier,
 t.position AS tank_position,m.position,p.site_id AS placement_site_id,d.site_id,
 p.started_at AS placement_started_at,
 EXISTS (SELECT 1 FROM container_module_placement prior WHERE prior.module_id=m.id AND prior.id<p.id) AS has_prior_placement
FROM container_module_identity i
LEFT JOIN container_module m ON m.id=i.module_id
LEFT JOIN container_tank t ON t.id=m.tank_id
LEFT JOIN device d ON d.id=t.device_id
LEFT JOIN container_module_placement p ON p.module_id=m.id AND p.ended_at IS NULL
WHERE i.org_id=sqlc.arg('org_id') AND i.reported_module_id=ANY(sqlc.arg('reported_module_ids')::text[]);

-- A contradictory claim withdraws command eligibility without assigning either
-- position to that identity. Source watermarks survive for a corrected report.
-- name: WithdrawContainerModuleIdentities :exec
WITH withdrawn AS (
 UPDATE container_module SET identity_confirmed=FALSE,state='unknown',state_observed_at=NULL
 WHERE id=ANY(sqlc.arg('module_ids')::bigint[]) RETURNING id
)
UPDATE container_module_latest_metrics SET hashrate_hs=NULL,power_w=NULL,temperature_c=NULL,efficiency_jh=NULL,coverage=NULL
WHERE module_id IN (SELECT id FROM withdrawn);
