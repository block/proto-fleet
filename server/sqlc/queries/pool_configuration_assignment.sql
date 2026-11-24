-- name: GetPoolAssignmentDevice :one
SELECT id FROM device WHERE org_id = $1 AND id = $2 AND deleted_at IS NULL FOR SHARE;

-- name: GetPoolAssignmentInstallation :one
SELECT i.id FROM hashboard_installation i
JOIN hashboard h ON h.id = i.hashboard_id
JOIN container_module m ON m.id = i.module_id
JOIN container_tank t ON t.id = m.tank_id
WHERE h.org_id = $1 AND i.id = $2 AND t.device_id = $3 AND i.device_id = $3
 AND i.removed_at IS NULL AND m.retired_at IS NULL AND t.present
FOR SHARE OF i, h, m, t;

-- name: GetPoolConfigurationAssignment :one
SELECT a.*, c.account_id FROM pool_configuration_assignment a
JOIN pool_configuration c ON c.organization_id = a.organization_id AND c.configuration_id = a.configuration_id
WHERE a.organization_id = $1 AND a.target_kind = $2 AND a.target_id = $3;

-- name: ListPoolConfigurationAssignmentsForDevices :many
SELECT d.id AS target_id, COALESCE(d.worker_name, '')::text AS worker_name, (d.worker_name IS NOT NULL)::boolean AS worker_name_set, ''::text AS worker_name_mode,
 COALESCE(a.configuration_id, '')::text AS configuration_id, a.retired_at, c.account_id
FROM device d
LEFT JOIN pool_configuration_assignment a ON a.organization_id = d.org_id AND a.target_kind = 'device' AND a.target_id = d.id
LEFT JOIN pool_configuration c ON c.organization_id = a.organization_id AND c.configuration_id = a.configuration_id
WHERE d.org_id = sqlc.arg(organization_id) AND d.deleted_at IS NULL
 AND d.id = ANY(sqlc.arg(device_ids)::bigint[]);

-- name: ListPoolConfigurationAssignments :many
SELECT a.* FROM pool_configuration_assignment a
JOIN pool_configuration c ON c.organization_id = a.organization_id AND c.configuration_id = a.configuration_id
WHERE a.organization_id = sqlc.arg(organization_id) AND a.configuration_id = sqlc.arg(configuration_id)
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint AND a.retired_at IS NULL
ORDER BY a.target_kind, a.target_id;

-- name: CreatePoolConfigurationOperation :one
INSERT INTO pool_configuration_operation(operation_id, organization_id, configuration_id, revision_id, actor_id)
VALUES($1, $2, $3, $4, $5) RETURNING *;

-- name: SetPoolConfigurationAssignment :one
-- Policy ordering uses save time after serialization locks, not transaction start.
-- Explicitly returning retired equipment must not resurrect its former intent
-- while the new reviewed operation is pending or if admission later fails.
WITH discarded AS (
 DELETE FROM pool_configuration_board_intent p USING pool_configuration_assignment a
 WHERE a.organization_id=$1 AND a.target_kind=$2 AND a.target_id=$3 AND a.retired_at IS NOT NULL
  AND p.organization_id=a.organization_id AND p.installation_id=a.target_id AND a.target_kind='hashboard_installation'
)
INSERT INTO pool_configuration_assignment(organization_id, target_kind, target_id, device_id, configuration_id, revision_id, version, operation_id, updated_at)
VALUES($1, $2, $3, $4, $5, $6, 1, $7, clock_timestamp())
ON CONFLICT (organization_id, target_kind, target_id) DO UPDATE SET
 configuration_id = EXCLUDED.configuration_id, revision_id = EXCLUDED.revision_id,
 version = pool_configuration_assignment.version + 1, operation_id = EXCLUDED.operation_id,
 inherited_worker_name = CASE WHEN pool_configuration_assignment.retired_at IS NULL THEN pool_configuration_assignment.inherited_worker_name ELSE NULL END,
 retired_at = NULL, updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: AdoptPoolConfigurationAssignment :execrows
-- Never overwrite even a retired assignment: returning hardware must not
-- inherit the previous occupant's desired configuration on a repeated import.
INSERT INTO pool_configuration_assignment(organization_id,target_kind,target_id,device_id,configuration_id,revision_id,version,adopted_at,inherited_worker_name)
VALUES($1,$2,$3,$4,$5,$6,1,CURRENT_TIMESTAMP,sqlc.narg(inherited_worker_name)::text)
ON CONFLICT (organization_id,target_kind,target_id) DO NOTHING;

-- name: ClearInheritedPoolWorkerName :execrows
-- Only explicit worker-name confirmation replaces inherited input. The caller
-- carries this version advance into its atomic reviewed assignment update.
UPDATE pool_configuration_assignment SET inherited_worker_name=NULL,version=version+1,updated_at=CURRENT_TIMESTAMP
WHERE organization_id=$1 AND target_kind=$2 AND target_id=$3 AND version=$4
 AND retired_at IS NULL AND inherited_worker_name IS NOT NULL;

-- name: HasPoolAssignmentKindConflict :one
SELECT EXISTS (
 SELECT 1 FROM pool_configuration_assignment WHERE organization_id=$1 AND device_id=$2
 AND target_kind<>$3 AND retired_at IS NULL
);

-- name: RetirePoolConfigurationAssignment :execrows
UPDATE pool_configuration_assignment a SET retired_at = clock_timestamp(), version = a.version + 1, updated_at = clock_timestamp()
FROM pool_configuration c
WHERE a.organization_id = sqlc.arg(organization_id) AND a.target_kind = sqlc.arg(target_kind) AND a.target_id = sqlc.arg(target_id)
 AND a.device_id = sqlc.arg(device_id)
 AND a.version = sqlc.arg(expected_version) AND a.retired_at IS NULL
 AND c.organization_id = a.organization_id AND c.configuration_id = a.configuration_id
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint;

-- name: SupersedePoolConfigurationTarget :exec
UPDATE pool_configuration_operation_target SET state = 'superseded', updated_at = CURRENT_TIMESTAMP
WHERE organization_id = $1 AND target_kind = $2 AND target_id = $3 AND state IN ('pending','applying','failed','different','applied_unverified');

-- name: CreatePoolConfigurationOperationTarget :one
INSERT INTO pool_configuration_operation_target(organization_id, operation_id, target_kind, target_id, assignment_version, reviewed_binding)
VALUES($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetPoolConfigurationOperation :one
SELECT o.* FROM pool_configuration_operation o
JOIN pool_configuration c ON c.organization_id = o.organization_id AND c.configuration_id = o.configuration_id
WHERE o.organization_id = sqlc.arg(organization_id) AND o.operation_id = sqlc.arg(operation_id)
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint;

-- name: ListPoolConfigurationOperationTargets :many
SELECT sqlc.embed(t), COALESCE(w.status,'')::text AS physical_attempt_status
FROM pool_configuration_operation_target t
LEFT JOIN device_write_attempt w ON w.attempt_id=t.physical_attempt_id AND w.organization_id=t.organization_id
WHERE t.organization_id = $1 AND t.operation_id = $2 ORDER BY t.target_kind, t.target_id;

-- name: ListPoolConfigurationOperationSites :many
-- Assignment identity is retained across retirement and reassignment. Use its
-- device's current committed site for read authorization, not the old reviewed
-- snapshot. This read must not take device locks held across hardware I/O;
-- actor and permission locks are retained by the enclosing read transaction.
SELECT d.site_id FROM pool_configuration_operation_target t
JOIN pool_configuration_assignment a ON a.organization_id=t.organization_id AND a.target_kind=t.target_kind AND a.target_id=t.target_id
JOIN device d ON d.org_id=a.organization_id AND d.id=a.device_id
WHERE t.organization_id=sqlc.arg(organization_id)::bigint AND t.operation_id=sqlc.arg(operation_id)::text
UNION ALL
SELECT d.site_id FROM pool_lifecycle_transition l JOIN device d ON d.org_id=l.organization_id AND d.id=l.device_id
WHERE l.organization_id=sqlc.arg(organization_id)::bigint AND l.transition_id::text=sqlc.arg(operation_id)::text;

-- name: ClaimPoolConfigurationTarget :one
UPDATE pool_configuration_operation_target t SET state = 'applying', attempts = attempts + 1, result_code = '', updated_at = CURRENT_TIMESTAMP
FROM pool_configuration_assignment a, pool_configuration c
WHERE t.organization_id = sqlc.arg(organization_id) AND t.operation_id = sqlc.arg(operation_id)
 AND t.target_kind = sqlc.arg(target_kind) AND t.target_id = sqlc.arg(target_id) AND t.state = 'pending'
 AND a.organization_id = t.organization_id AND a.target_kind = t.target_kind AND a.target_id = t.target_id
 AND a.device_id = sqlc.arg(device_id)
 AND a.operation_id = t.operation_id AND a.version = t.assignment_version AND a.retired_at IS NULL
 AND c.organization_id = a.organization_id AND c.configuration_id = a.configuration_id
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint
RETURNING t.attempts;

-- name: FinishPoolConfigurationTarget :execrows
UPDATE pool_configuration_operation_target t SET state = sqlc.arg(state), result_code = sqlc.arg(result_code), updated_at = CURRENT_TIMESTAMP
FROM pool_configuration_assignment a, pool_configuration c
WHERE t.organization_id = sqlc.arg(organization_id) AND t.operation_id = sqlc.arg(operation_id)
 AND t.target_kind = sqlc.arg(target_kind) AND t.target_id = sqlc.arg(target_id)
 AND t.state = 'applying' AND t.attempts = sqlc.arg(attempt)
 AND a.organization_id = t.organization_id AND a.target_kind = t.target_kind AND a.target_id = t.target_id
 AND a.device_id = sqlc.arg(device_id)
 AND a.operation_id = t.operation_id AND a.version = t.assignment_version AND a.retired_at IS NULL
 AND c.organization_id = a.organization_id AND c.configuration_id = a.configuration_id
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint;

-- name: RetryPoolConfigurationTarget :execrows
UPDATE pool_configuration_operation_target t SET state = 'pending', result_code = '', updated_at = CURRENT_TIMESTAMP
FROM pool_configuration_assignment a, pool_configuration c
WHERE t.organization_id = sqlc.arg(organization_id) AND t.operation_id = sqlc.arg(operation_id)
 AND t.target_kind = sqlc.arg(target_kind) AND t.target_id = sqlc.arg(target_id) AND t.state IN ('failed','different','applied_unverified')
 AND t.result_code <> 'write_uncertain'
 AND a.organization_id = t.organization_id AND a.target_kind = t.target_kind AND a.target_id = t.target_id
 AND a.device_id = sqlc.arg(device_id)
 AND a.operation_id = t.operation_id AND a.version = t.assignment_version AND a.retired_at IS NULL
 AND c.organization_id = a.organization_id AND c.configuration_id = a.configuration_id
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint;

-- name: ListActivePoolAssignmentsForTargets :many
SELECT a.target_kind, a.target_id FROM pool_configuration_assignment a
WHERE a.organization_id = sqlc.arg(organization_id) AND a.retired_at IS NULL
 AND ((a.target_kind = 'device' AND a.target_id = ANY(sqlc.arg(device_ids)::bigint[]))
  OR (a.target_kind = 'hashboard_installation' AND a.target_id = ANY(sqlc.arg(installation_ids)::bigint[])));

-- name: LockCurrentPoolConfigurationOperationTarget :one
-- Acquire only after the equipment reviewer holds physical admission locks.
-- Keep outcome changes from racing a matching-name retry confirmation.
SELECT t.operation_id FROM pool_configuration_operation_target t
JOIN pool_configuration_assignment a ON a.organization_id=t.organization_id AND a.target_kind=t.target_kind AND a.target_id=t.target_id
 AND a.operation_id=t.operation_id AND a.version=t.assignment_version
WHERE t.organization_id=$1 AND t.target_kind=$2 AND t.target_id=$3 AND a.retired_at IS NULL
FOR UPDATE OF t;
