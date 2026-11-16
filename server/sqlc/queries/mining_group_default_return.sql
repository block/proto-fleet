-- name: ListDefaultReturnBoards :many
-- Include allocated and unavailable boards so exact recall selections fail
-- closed instead of silently moving a different set of boards.
SELECT h.id AS hashboard_id,i.id AS installation_id,COALESCE(i.metadata->>'macAddress','')::text AS mac_address,
 d.id AS device_id,d.device_identifier,COALESCE(a.client_id,0)::bigint AS client_id,
 COALESCE(a.revision,0)::bigint AS allocation_revision,COALESCE(pa.version,0)::bigint AS pool_assignment_version,
 (EXISTS (
  SELECT 1 FROM pool_configuration_operation_target work WHERE work.organization_id=h.org_id
   AND ((work.target_kind='hashboard_installation' AND work.target_id=i.id) OR (work.target_kind='device' AND work.target_id=d.id))
   AND (work.state IN ('pending','applying','uncertain') OR (work.state='failed' AND work.result_code='write_uncertain'))
 ) OR EXISTS (
  SELECT 1 FROM pool_lifecycle_transition policy WHERE policy.organization_id=h.org_id AND policy.device_id=d.id
   AND (policy.asset_kind=0 OR (policy.asset_kind=7 AND policy.asset_id=h.id))
   AND policy.state IN ('pending','deferred','ready','applying','uncertain')
 ))::boolean AS pending_pool_work
FROM hashboard h JOIN hashboard_installation i ON i.hashboard_id=h.id AND i.removed_at IS NULL
JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id
JOIN device d ON d.id=t.device_id AND d.org_id=h.org_id
LEFT JOIN hosted_account_asset a ON a.organization_id=h.org_id AND a.kind=7 AND a.asset_id=h.id
LEFT JOIN pool_configuration_assignment pa ON pa.organization_id=h.org_id AND pa.target_kind='hashboard_installation' AND pa.target_id=i.id
WHERE h.org_id=sqlc.arg(organization_id) AND d.deleted_at IS NULL
 AND (sqlc.arg(device_identifier)::text='' OR d.device_identifier=sqlc.arg(device_identifier))
 AND (COALESCE(cardinality(sqlc.arg(hashboard_ids)::bigint[]),0)=0 OR h.id=ANY(sqlc.arg(hashboard_ids)::bigint[]))
ORDER BY d.id,h.id;

-- name: SaveMiningGroupDefaultReturn :exec
UPDATE container_mining_group_state SET default_return=$3 WHERE organization_id=$1 AND device_id=$2;

-- name: ListDefaultReturnBoardNames :many
-- Use the same serial/MAC/Fleet-ID order as hosted_mining_asset, without its
-- telemetry joins. Display names do not change a saved transfer's identity.
SELECT h.id AS hashboard_id,
 ('Hashboard '||COALESCE((SELECT identity.value FROM hashboard_identity identity
  WHERE identity.org_id=h.org_id AND identity.hashboard_id=h.id
  ORDER BY (identity.kind='serial') DESC,identity.value LIMIT 1),h.id::text))::text AS name
FROM hashboard h WHERE h.org_id=sqlc.arg(organization_id)
 AND h.id=ANY(sqlc.arg(hashboard_ids)::bigint[]) ORDER BY h.id;

-- name: HasPendingDefaultReturnPoolPolicy :one
SELECT EXISTS (
 SELECT 1 FROM pool_lifecycle_transition l WHERE l.organization_id=sqlc.arg(organization_id) AND l.device_id=sqlc.arg(device_id)
 AND (l.state IN ('pending','deferred','ready','applying','uncertain') OR (l.state='failed' AND l.result_code='clear_not_dispatched'))
) OR EXISTS (
 SELECT 1 FROM pool_configuration_operation_target t
 JOIN pool_configuration_assignment a ON a.organization_id=t.organization_id AND a.target_kind=t.target_kind AND a.target_id=t.target_id
 WHERE t.organization_id=sqlc.arg(organization_id) AND a.device_id=sqlc.arg(device_id)
 AND (t.state IN ('pending','applying','uncertain') OR (t.state='failed' AND t.result_code='write_uncertain'))
)::boolean AS pending;

-- name: RetireDefaultReturnAssignments :exec
-- A confirmed move supersedes the previous board policy, including completed
-- clear intent. Keep history, but never reapply it in a later workflow.
UPDATE pool_configuration_assignment a SET retired_at=COALESCE(a.retired_at,clock_timestamp()),version=a.version+1,updated_at=clock_timestamp()
WHERE a.organization_id=sqlc.arg(organization_id) AND a.device_id=sqlc.arg(device_id)
 AND a.target_kind='hashboard_installation' AND a.target_id=ANY(sqlc.arg(installation_ids)::bigint[])
 AND (a.retired_at IS NULL OR EXISTS (
  SELECT 1 FROM pool_configuration_operation_target t
  JOIN pool_configuration_operation o ON o.organization_id=t.organization_id AND o.operation_id=t.operation_id AND o.kind='clear'
  WHERE t.organization_id=a.organization_id
   AND t.target_kind=a.target_kind AND t.target_id=a.target_id
   AND t.state='completed' AND t.assignment_version+1=a.version
 ));

-- name: SupersedeDefaultReturnClears :exec
UPDATE pool_lifecycle_transition SET state='superseded',result_code='returned_to_default',updated_at=clock_timestamp()
WHERE organization_id=sqlc.arg(organization_id) AND device_id=sqlc.arg(device_id) AND target_kind='hashboard_installation'
 AND target_id=ANY(sqlc.arg(installation_ids)::bigint[]) AND state IN ('completed','verified');

-- name: DeleteDefaultReturnBoardIntents :exec
DELETE FROM pool_configuration_board_intent WHERE organization_id=sqlc.arg(organization_id) AND device_id=sqlc.arg(device_id) AND installation_id=ANY(sqlc.arg(installation_ids)::bigint[]);

-- name: DeleteDefaultReturnLegacyIntents :exec
DELETE FROM hashboard_pool_intent WHERE organization_id=sqlc.arg(organization_id) AND device_id=sqlc.arg(device_id) AND installation_id=ANY(sqlc.arg(installation_ids)::bigint[]);
