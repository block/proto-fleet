-- Serialize cross-controller identity/placement reconciliation within an org.
-- name: LockHashboardOrganization :one
SELECT o.id FROM organization o JOIN device d ON d.org_id = o.id
WHERE d.device_identifier = sqlc.arg('identifier') AND d.deleted_at IS NULL
FOR NO KEY UPDATE OF o;

-- name: FindHashboardIdentities :many
SELECT i.kind, i.value, i.hashboard_id, h.last_seen_at FROM hashboard_identity i JOIN hashboard h ON h.id=i.hashboard_id
WHERE i.org_id = sqlc.arg('org_id') AND i.hashboard_id IN (
 SELECT matched.hashboard_id FROM hashboard_identity matched
 CROSS JOIN jsonb_to_recordset(sqlc.arg('boards')::jsonb) AS x(mac_address text, serial text)
 WHERE matched.org_id = sqlc.arg('org_id') AND ((matched.kind = 'mac' AND matched.value = x.mac_address) OR (matched.kind = 'serial' AND matched.value = x.serial))
);

-- The caller rejects aliases that resolve to different existing hardware.
-- name: EnsureHashboards :many
WITH input AS MATERIALIZED (
 SELECT COALESCE(value->>'mac_address','')::text AS mac_address, COALESCE(value->>'serial','')::text AS serial, COALESCE(NULLIF((value->>'board_id')::bigint, 0), nextval('hashboard_id_seq'))::bigint AS id
 FROM jsonb_array_elements(sqlc.arg('boards')::jsonb)
 WHERE value->>'mac_address' <> '' OR value->>'serial' <> ''
), boards AS (
 INSERT INTO hashboard(id, org_id, first_seen_at, last_seen_at)
 SELECT x.id, sqlc.arg('org_id'), sqlc.arg('observed_at')::timestamptz, sqlc.arg('observed_at')::timestamptz FROM input x
 ON CONFLICT (id) DO UPDATE SET last_seen_at = GREATEST(hashboard.last_seen_at, EXCLUDED.last_seen_at)
 RETURNING id
), aliases AS (
 INSERT INTO hashboard_identity(org_id, kind, value, hashboard_id)
 SELECT sqlc.arg('org_id'), a.kind, a.value, x.id FROM input x JOIN boards b ON b.id = x.id
 CROSS JOIN LATERAL (VALUES ('mac', x.mac_address), ('serial', x.serial)) a(kind, value)
 WHERE a.value <> '' ON CONFLICT DO NOTHING
)
SELECT x.id::bigint AS id, x.mac_address::text AS mac_address, x.serial::text AS serial FROM input x JOIN boards b ON b.id=x.id;

-- name: GetHashboardInstallations :many
SELECT i.*, t.device_id, h.last_seen_at AS board_last_seen_at,
 EXISTS (SELECT 1 FROM hashboard_installation prior WHERE prior.hashboard_id=i.hashboard_id
   AND (prior.installed_at,prior.id)<(i.installed_at,i.id)) AS has_prior_installation
FROM hashboard_installation i JOIN hashboard h ON h.id=i.hashboard_id
JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id
WHERE h.org_id=sqlc.arg('org_id') AND i.removed_at IS NULL
AND (t.device_id=sqlc.arg('device_id') OR i.hashboard_id=ANY(sqlc.arg('board_ids')::bigint[]));

-- name: ListHashboardModules :many
SELECT m.id, m.position, m.retired_at, t.position AS tank_position, d.site_id
FROM container_module m JOIN container_tank t ON t.id=m.tank_id JOIN device d ON d.id=t.device_id
WHERE t.device_id=sqlc.arg('device_id');

-- name: CloseHashboardInstallations :exec
WITH closed AS (
 UPDATE hashboard_installation SET removed_at=sqlc.arg('observed_at')::timestamptz
 WHERE id=ANY(sqlc.arg('ids')::bigint[]) AND removed_at IS NULL AND last_seen_at <= sqlc.arg('observed_at')::timestamptz
 RETURNING hashboard_id
)
UPDATE hashboard SET last_seen_at=GREATEST(last_seen_at,sqlc.arg('observed_at')::timestamptz)
WHERE id IN (SELECT hashboard_id FROM closed);

-- name: UpsertHashboardSlots :exec
INSERT INTO hashboard_slot(module_id, hashboard_id, position, occupancy, observed_at, health_observed_at, state, state_observed_at, mining_state, isolated, unavailable_reason)
SELECT x.module_id, NULLIF(x.board_id,0), x.position, x.occupancy, sqlc.arg('observed_at')::timestamptz, x.time, x.state, x.state_observed_at, COALESCE(x.mining_state,''), x.isolated, COALESCE(x.unavailable_reason,'')
FROM jsonb_to_recordset(sqlc.arg('slots')::jsonb) AS x(module_id bigint, board_id bigint, position int, occupancy text, time timestamptz, state text, state_observed_at timestamptz, mining_state text, isolated boolean, unavailable_reason text)
ON CONFLICT (module_id, position) DO UPDATE SET hashboard_id=EXCLUDED.hashboard_id, occupancy=EXCLUDED.occupancy, observed_at=EXCLUDED.observed_at,
 health_observed_at=CASE WHEN hashboard_slot.hashboard_id=EXCLUDED.hashboard_id AND EXCLUDED.health_observed_at IS NOT NULL THEN GREATEST(hashboard_slot.health_observed_at,EXCLUDED.health_observed_at) ELSE EXCLUDED.health_observed_at END,
 state=CASE WHEN hashboard_slot.hashboard_id=EXCLUDED.hashboard_id AND EXCLUDED.state <> 'unknown' AND hashboard_slot.state_observed_at > EXCLUDED.state_observed_at THEN hashboard_slot.state ELSE EXCLUDED.state END,
 state_observed_at=CASE WHEN hashboard_slot.hashboard_id=EXCLUDED.hashboard_id AND EXCLUDED.state <> 'unknown' THEN GREATEST(hashboard_slot.state_observed_at,EXCLUDED.state_observed_at) ELSE EXCLUDED.state_observed_at END,
 mining_state=EXCLUDED.mining_state, isolated=EXCLUDED.isolated, unavailable_reason=EXCLUDED.unavailable_reason
WHERE hashboard_slot.observed_at < EXCLUDED.observed_at;

-- name: UpsertHashboardInstallations :exec
INSERT INTO hashboard_installation(hashboard_id, module_id, position, site_id, installed_at, last_seen_at, metadata)
SELECT x.board_id, x.module_id, x.position, x.site_id, sqlc.arg('observed_at')::timestamptz, sqlc.arg('observed_at')::timestamptz, COALESCE(x.metadata, '{}')
FROM jsonb_to_recordset(sqlc.arg('boards')::jsonb) AS x(board_id bigint, module_id bigint, position int, site_id bigint, metadata jsonb)
ON CONFLICT (hashboard_id) WHERE removed_at IS NULL DO UPDATE SET last_seen_at=EXCLUDED.last_seen_at, metadata=EXCLUDED.metadata
WHERE hashboard_installation.module_id=EXCLUDED.module_id AND hashboard_installation.position=EXCLUDED.position
  AND hashboard_installation.last_seen_at < EXCLUDED.last_seen_at;

-- name: InsertHashboardMetrics :exec
WITH inserted AS (
 INSERT INTO hashboard_metrics(hashboard_id, installation_id, time, collected_at, state, state_observed_at, mining_state, isolated, hashrate_hs, power_w, efficiency_jh, temperature_c, hashrail_ok, refclks_ok, health)
 SELECT x.board_id, i.id, x.time, sqlc.arg('collected_at')::timestamptz, CASE WHEN evidence.current_state THEN x.state ELSE 'unknown' END, CASE WHEN evidence.current_state THEN x.state_observed_at ELSE NULL END, COALESCE(x.mining_state,''), x.isolated, x.hashrate_hs, x.power_w, x.efficiency_jh, x.temperature_c, x.hashrail_ok, x.refclks_ok, CASE WHEN evidence.current_state THEN x.health ELSE NULL END
 FROM jsonb_to_recordset(sqlc.arg('boards')::jsonb) AS x(board_id bigint, module_id bigint, position int, time timestamptz, state text, state_observed_at timestamptz, mining_state text, isolated boolean, hashrate_hs double precision, power_w double precision, efficiency_jh double precision, temperature_c double precision, hashrail_ok boolean, refclks_ok boolean, health jsonb)
 JOIN hashboard_installation i ON i.hashboard_id=x.board_id AND i.module_id=x.module_id AND i.position=x.position AND i.removed_at IS NULL
 CROSS JOIN LATERAL (SELECT x.state_observed_at >= i.installed_at OR NOT EXISTS (SELECT 1 FROM hashboard_installation prior WHERE prior.hashboard_id=i.hashboard_id AND (prior.installed_at,prior.id)<(i.installed_at,i.id)) AS current_state) evidence
 WHERE i.last_seen_at=sqlc.arg('collected_at')::timestamptz AND x.time IS NOT NULL
 -- Initial discovery can observe an asynchronous sample. After a move, cached
 -- evidence from a prior placement must never acquire the new site's authority.
 AND (x.time >= i.installed_at OR NOT EXISTS (SELECT 1 FROM hashboard_installation prior WHERE prior.hashboard_id=i.hashboard_id AND (prior.installed_at,prior.id)<(i.installed_at,i.id)))
 ON CONFLICT DO NOTHING RETURNING hashboard_id, installation_id, time, collected_at, state, state_observed_at, mining_state, isolated, hashrate_hs, power_w, efficiency_jh, temperature_c, hashrail_ok, refclks_ok, health
)
INSERT INTO hashboard_latest_metrics (hashboard_id, installation_id, time, collected_at, state, state_observed_at, mining_state, isolated, hashrate_hs, power_w, efficiency_jh, temperature_c, hashrail_ok, refclks_ok, health)
SELECT hashboard_id, installation_id, time, collected_at, state, state_observed_at, mining_state, isolated, hashrate_hs, power_w, efficiency_jh, temperature_c, hashrail_ok, refclks_ok, health FROM inserted
ON CONFLICT (hashboard_id) DO UPDATE SET installation_id=EXCLUDED.installation_id, time=EXCLUDED.time, collected_at=EXCLUDED.collected_at,
 state=EXCLUDED.state, state_observed_at=EXCLUDED.state_observed_at, mining_state=EXCLUDED.mining_state, isolated=EXCLUDED.isolated,
 hashrate_hs=EXCLUDED.hashrate_hs, power_w=EXCLUDED.power_w, efficiency_jh=EXCLUDED.efficiency_jh, temperature_c=EXCLUDED.temperature_c, hashrail_ok=EXCLUDED.hashrail_ok, refclks_ok=EXCLUDED.refclks_ok, health=EXCLUDED.health
WHERE (hashboard_latest_metrics.time, hashboard_latest_metrics.collected_at) < (EXCLUDED.time, EXCLUDED.collected_at)
 OR (hashboard_latest_metrics.installation_id <> EXCLUDED.installation_id AND hashboard_latest_metrics.collected_at < EXCLUDED.collected_at);
