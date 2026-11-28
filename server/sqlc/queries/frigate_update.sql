-- name: GetFrigateUpdateTarget :one
-- A paired CT1 container (device class 2) addressed by Fleet device identifier.
SELECT d.id, d.device_identifier, d.site_id
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
WHERE d.device_identifier = sqlc.arg('device_identifier') AND d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class = 2
  AND dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD');

-- name: LockFrigateUpdateTarget :one
SELECT d.id, d.device_identifier, d.site_id
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
WHERE d.device_identifier = sqlc.arg('device_identifier') AND d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class = 2
  AND dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')
FOR UPDATE OF d;

-- name: GetFrigateUpdateDeviceSite :one
SELECT device_identifier, site_id FROM device WHERE id = $1 AND deleted_at IS NULL;

-- name: InsertFrigateUpdatePackage :one
INSERT INTO frigate_update_package (
    id, org_id, kind, filename, version, architecture, sha256, size_bytes, repository,
    release_key_id, min_installed_version, issued_at, statement, signature, uploaded_by
) VALUES (
    sqlc.arg('id'), sqlc.arg('org_id'), sqlc.arg('kind'), sqlc.arg('filename'), sqlc.arg('version'), sqlc.arg('architecture'),
    sqlc.arg('sha256'), sqlc.arg('size_bytes'), sqlc.arg('repository'), sqlc.arg('release_key_id'),
    sqlc.arg('min_installed_version'), sqlc.arg('issued_at'), sqlc.arg('statement'), sqlc.arg('signature'),
    sqlc.narg('uploaded_by')
)
RETURNING *;

-- name: GetFrigateUpdatePackageBySHA256 :one
SELECT * FROM frigate_update_package
WHERE org_id = $1 AND sha256 = $2 AND deleted_at IS NULL;

-- name: GetFrigateUpdatePackage :one
-- Includes deleted packages so job history keeps resolving them.
SELECT p.*, COALESCE(u.username, '')::text AS uploaded_by_username
FROM frigate_update_package p
LEFT JOIN "user" u ON u.id = p.uploaded_by
WHERE p.org_id = $1 AND p.id = $2;

-- name: LockFrigateUpdatePackage :one
SELECT * FROM frigate_update_package
WHERE org_id = $1 AND id = $2 AND deleted_at IS NULL
FOR UPDATE;

-- name: ListFrigateUpdatePackages :many
SELECT p.*, COALESCE(u.username, '')::text AS uploaded_by_username
FROM frigate_update_package p
LEFT JOIN "user" u ON u.id = p.uploaded_by
WHERE p.org_id = $1 AND p.deleted_at IS NULL
ORDER BY p.uploaded_at DESC, p.id;

-- name: SoftDeleteFrigateUpdatePackage :exec
UPDATE frigate_update_package SET deleted_at = CURRENT_TIMESTAMP
WHERE org_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: FrigateUpdatePackageInUse :one
SELECT EXISTS (
    SELECT 1 FROM frigate_update_job
    WHERE package_id = $1
      AND (state IN ('staging', 'staged', 'starting', 'running') OR (state = 'failed' AND retryable))
)::boolean;

-- name: AnyDeviceHasBlockingFrigateUpdateJob :one
-- Device retirement refuses while a rollout is in progress or can still be
-- retried, because the job addresses the container by its device row. The
-- predicate matches idx_frigate_update_job_device_blocking.
SELECT EXISTS (
    SELECT 1 FROM frigate_update_job
    WHERE device_id = ANY(sqlc.arg(device_ids)::bigint[])
      AND (state IN ('staging', 'staged', 'starting', 'running') OR (state = 'failed' AND retryable))
)::boolean;

-- name: InsertFrigateUpdateJob :one
INSERT INTO frigate_update_job (
    id, org_id, device_id, package_id, state, stage_only, target_version, expected_instance_ids,
    request_key, actor_user_id, actor_external_id, actor_username
) VALUES (
    sqlc.arg('id'), sqlc.arg('org_id'), sqlc.arg('device_id'), sqlc.arg('package_id'), 'staging',
    sqlc.arg('stage_only'), sqlc.arg('target_version'), sqlc.arg('expected_instance_ids')::text[],
    sqlc.arg('request_key'), sqlc.arg('actor_user_id'), sqlc.arg('actor_external_id'), sqlc.arg('actor_username')
)
RETURNING *;

-- name: InsertFrigateUpdateInstance :exec
INSERT INTO frigate_update_instance (job_id, instance_id, size_bytes)
VALUES ($1, $2, $3);

-- name: GetFrigateUpdateJob :one
SELECT j.*
FROM frigate_update_job j
WHERE j.org_id = $1 AND j.id = $2;

-- name: GetFrigateUpdateJobByRequestKey :one
SELECT j.*
FROM frigate_update_job j
WHERE j.org_id = $1 AND j.request_key = $2;

-- name: GetBlockingFrigateUpdateJobForDevice :one
SELECT j.*
FROM frigate_update_job j
WHERE j.device_id = $1
  AND (j.state IN ('staging', 'staged', 'starting', 'running') OR (j.state = 'failed' AND j.retryable));

-- name: LockFrigateUpdateJob :one
SELECT * FROM frigate_update_job WHERE id = $1 FOR UPDATE;

-- name: GetFrigateUpdateJobPackageKind :one
-- The package kind selects the job's state machine.
SELECT p.kind
FROM frigate_update_job j
JOIN frigate_update_package p ON p.id = j.package_id
WHERE j.id = $1;

-- name: ListFrigateUpdateJobsForDevice :many
SELECT j.*
FROM frigate_update_job j
WHERE j.org_id = $1 AND j.device_id = $2
ORDER BY j.created_at DESC, j.id
LIMIT $3;

-- name: ListRunnableFrigateUpdateJobs :many
-- Jobs the runner must advance. Retryable failures stay listed so a rollout
-- that an operator retries elsewhere (or on the host) is noticed.
SELECT j.*
FROM frigate_update_job j
WHERE j.state IN ('staging', 'staged', 'starting', 'running')
   OR (j.state = 'failed' AND j.retryable)
ORDER BY j.created_at, j.id;

-- name: ListFrigateUpdateCleanupJobs :many
-- Least recently attempted first (never attempted before any), so a job whose
-- controllers cannot be reached does not hold back the ones behind it.
SELECT j.*
FROM frigate_update_job j
WHERE j.cleanup_pending
ORDER BY j.cleanup_attempted_at NULLS FIRST, j.updated_at, j.id
LIMIT 20;

-- name: MarkFrigateUpdateCleanupAttempted :exec
UPDATE frigate_update_job SET cleanup_attempted_at = sqlc.arg('attempted_at')
WHERE id = sqlc.arg('id');

-- name: ListFrigateUpdateInstances :many
SELECT * FROM frigate_update_instance WHERE job_id = $1 ORDER BY instance_id;

-- name: ListFrigateUpdateInstancesForJobs :many
SELECT * FROM frigate_update_instance WHERE job_id = ANY(sqlc.arg('job_ids')::uuid[]) ORDER BY job_id, instance_id;

-- name: UpdateFrigateUpdateJob :exec
-- Callers hold the job row lock and have decided every field.
UPDATE frigate_update_job SET
    state = sqlc.arg('state'),
    rollout_phase = sqlc.arg('rollout_phase'),
    from_version = sqlc.arg('from_version'),
    driver_instance_id = sqlc.arg('driver_instance_id'),
    error_code = sqlc.arg('error_code'),
    error_message = sqlc.arg('error_message'),
    next_step = sqlc.arg('next_step'),
    retryable = sqlc.arg('retryable'),
    cancellable = sqlc.arg('cancellable'),
    disruption_started = sqlc.arg('disruption_started'),
    served_by_maintenance = sqlc.arg('served_by_maintenance'),
    start_attempted = sqlc.arg('start_attempted'),
    action_counter = sqlc.arg('action_counter'),
    start_attempted_at = sqlc.narg('start_attempted_at'),
    start_lease_until = sqlc.narg('start_lease_until'),
    cancel_requested_at = sqlc.narg('cancel_requested_at'),
    cancel_request_key = sqlc.arg('cancel_request_key'),
    cancel_request_actor = sqlc.arg('cancel_request_actor'),
    cleanup_pending = sqlc.arg('cleanup_pending'),
    rollout_updated_at = sqlc.narg('rollout_updated_at'),
    last_polled_at = sqlc.narg('last_polled_at'),
    last_contact_at = sqlc.narg('last_contact_at'),
    unreachable_since = sqlc.narg('unreachable_since'),
    completed_at = sqlc.narg('completed_at'),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id');

-- name: UpdateFrigateUpdateInstance :exec
UPDATE frigate_update_instance SET
    stage_state = sqlc.arg('stage_state'),
    manifest_sha256 = sqlc.arg('manifest_sha256'),
    staged_bytes = sqlc.arg('staged_bytes'),
    agent_phase = sqlc.arg('agent_phase'),
    last_contact = sqlc.narg('last_contact'),
    contact_stale = sqlc.arg('contact_stale'),
    running_version = sqlc.arg('running_version'),
    migration_executed = sqlc.arg('migration_executed'),
    error_code = sqlc.arg('error_code'),
    error_message = sqlc.arg('error_message'),
    updated_at = CURRENT_TIMESTAMP
WHERE job_id = sqlc.arg('job_id') AND instance_id = sqlc.arg('instance_id');

-- name: UpdateFrigateUpdateInstanceProtoOS :exec
-- What one controller reports about a ProtoOS job. The staging columns are
-- Fleet's and stay as they are.
UPDATE frigate_update_instance SET
    protoos_job_state = sqlc.arg('protoos_job_state'),
    protoos_installed_version = sqlc.arg('protoos_installed_version'),
    protoos_served_release_tag = sqlc.arg('protoos_served_release_tag'),
    last_contact = sqlc.narg('last_contact'),
    error_code = sqlc.arg('error_code'),
    error_message = sqlc.arg('error_message'),
    updated_at = CURRENT_TIMESTAMP
WHERE job_id = sqlc.arg('job_id') AND instance_id = sqlc.arg('instance_id');

-- name: UpdateFrigateUpdateInstanceStaging :execrows
-- Staging progress is written per acknowledgement without the job lock. It
-- affects no row once the job has left staging, which tells the uploader to stop.
UPDATE frigate_update_instance SET
    stage_state = sqlc.arg('stage_state'),
    manifest_sha256 = sqlc.arg('manifest_sha256'),
    staged_bytes = sqlc.arg('staged_bytes'),
    error_code = sqlc.arg('error_code'),
    error_message = sqlc.arg('error_message'),
    updated_at = CURRENT_TIMESTAMP
WHERE job_id = sqlc.arg('job_id') AND instance_id = sqlc.arg('instance_id')
  AND EXISTS (SELECT 1 FROM frigate_update_job j WHERE j.id = sqlc.arg('job_id') AND j.state = 'staging');

-- name: GetFrigateUpdateAction :one
SELECT * FROM frigate_update_action
WHERE job_id = $1 AND kind = $2 AND request_key = $3;

-- name: CountFrigateUpdateActions :one
SELECT count(*)::integer FROM frigate_update_action WHERE job_id = $1;

-- name: InsertFrigateUpdateAction :exec
INSERT INTO frigate_update_action (job_id, kind, request_key, counter)
VALUES ($1, $2, $3, $4);

-- name: MarkFrigateUpdateActionLocal :exec
UPDATE frigate_update_action SET local = TRUE
WHERE job_id = $1 AND kind = $2 AND request_key = $3;

-- name: UpdateFrigateUpdatePackageRelease :exec
-- Re-uploading the same bytes with a newly verified statement replaces the
-- statement-derived fields. Callers hold the package lock and have checked that
-- no blocking job uses the package.
UPDATE frigate_update_package SET
    version = sqlc.arg('version'),
    architecture = sqlc.arg('architecture'),
    repository = sqlc.arg('repository'),
    release_key_id = sqlc.arg('release_key_id'),
    min_installed_version = sqlc.arg('min_installed_version'),
    issued_at = sqlc.arg('issued_at'),
    statement = sqlc.arg('statement'),
    signature = sqlc.arg('signature')
WHERE org_id = sqlc.arg('org_id') AND id = sqlc.arg('id') AND deleted_at IS NULL;

-- name: FrigateUpdatePackageWantsBlob :one
-- A blob belongs to a package that has not been deleted. Soft-deleted rows and
-- unknown IDs are orphans.
SELECT EXISTS (
    SELECT 1 FROM frigate_update_package WHERE id = $1 AND deleted_at IS NULL
)::boolean;
