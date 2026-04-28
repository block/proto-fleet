-- name: GetSchedule :one
SELECT s.*, u.username AS created_by_username
FROM schedule s
LEFT JOIN "user" u ON u.id = s.created_by
WHERE s.org_id = $1
  AND s.id = $2
  AND s.deleted_at IS NULL;

-- name: ListSchedules :many
SELECT s.*, u.username AS created_by_username
FROM schedule s
LEFT JOIN "user" u ON u.id = s.created_by
WHERE s.org_id = $1
  AND s.deleted_at IS NULL
  AND (sqlc.narg('status')::text IS NULL OR s.status = sqlc.narg('status'))
  AND (sqlc.narg('action')::text IS NULL OR s.action = sqlc.narg('action'))
ORDER BY s.priority, s.id;

-- name: CreateSchedule :one
INSERT INTO schedule (
    org_id, name, action, action_config, schedule_type,
    recurrence, start_date, start_time, end_time,
    end_date, timezone, status, priority,
    created_by, next_run_at
)
VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12, $13,
    $14, $15
)
RETURNING id;

-- name: UpdateSchedule :execrows
UPDATE schedule
SET name          = $1,
    action        = $2,
    action_config = $3,
    schedule_type = $4,
    recurrence    = $5,
    start_date    = $6,
    start_time    = $7,
    end_time      = $8,
    end_date      = $9,
    timezone      = $10,
    next_run_at   = $11,
    status        = $12
WHERE org_id = $13
  AND id = $14
  AND deleted_at IS NULL
  AND status != 'running';

-- name: SoftDeleteSchedule :execrows
UPDATE schedule
SET deleted_at = CURRENT_TIMESTAMP
WHERE org_id = $1
  AND id = $2
  AND deleted_at IS NULL;

-- name: NegateSchedulePriorities :exec
UPDATE schedule s
SET priority = -t.new_priority
FROM (
    SELECT id, ordinality::int AS new_priority
    FROM unnest(@ids::bigint[]) WITH ORDINALITY AS u(id, ordinality)
) t
WHERE s.id = t.id
  AND s.org_id = $1
  AND s.deleted_at IS NULL;

-- name: SetSchedulePriorities :exec
UPDATE schedule s
SET priority = t.new_priority
FROM (
    SELECT id, ordinality::int AS new_priority
    FROM unnest(@ids::bigint[]) WITH ORDINALITY AS u(id, ordinality)
) t
WHERE s.id = t.id
  AND s.org_id = $1
  AND s.deleted_at IS NULL;

-- name: UpdateScheduleAfterRun :exec
UPDATE schedule
SET last_run_at = $1,
    next_run_at = $2,
    status      = $3
WHERE id = $4
  AND deleted_at IS NULL
  AND status IN ('active', 'running');

-- name: GetMaxPriority :one
SELECT COALESCE(MAX(priority), 0)::int AS max_priority
FROM schedule
WHERE org_id = $1
  AND deleted_at IS NULL;

-- name: GetRunningPowerTargetScheduleOverlaps :many
WITH requested AS (
    SELECT UNNEST(sqlc.arg('device_identifiers')::text[]) AS device_identifier
)
SELECT DISTINCT
    s.id AS schedule_id,
    s.priority AS schedule_priority,
    r.device_identifier::text AS device_identifier
FROM schedule s
JOIN schedule_target st ON st.schedule_id = s.id
JOIN requested r ON (
    (st.target_type = 'miner' AND st.target_id = r.device_identifier)
    OR (
        st.target_type IN ('rack', 'group')
        AND EXISTS (
            SELECT 1
            FROM device_set_membership dsm
            JOIN device_set ds ON ds.id = dsm.device_set_id
            WHERE dsm.org_id = s.org_id
              AND ds.org_id = s.org_id
              AND ds.deleted_at IS NULL
              AND dsm.device_set_id = CASE WHEN st.target_id ~ '^[0-9]+$' THEN st.target_id::bigint ELSE NULL END
              AND dsm.device_set_type::text = st.target_type
              AND dsm.device_identifier = r.device_identifier
        )
    )
)
WHERE s.org_id = $1
  AND s.status = 'running'
  AND s.action = 'set_power_target'
  AND s.deleted_at IS NULL
ORDER BY s.priority, s.id, r.device_identifier;

-- name: CreateScheduleTarget :exec
INSERT INTO schedule_target (schedule_id, target_type, target_id)
SELECT $1, $2, $3
FROM schedule
WHERE id = $1
  AND org_id = $4
  AND deleted_at IS NULL;

-- name: GetScheduleTargets :many
SELECT st.*
FROM schedule_target st
JOIN schedule s ON s.id = st.schedule_id
WHERE s.org_id = $1
  AND st.schedule_id = $2
  AND s.deleted_at IS NULL;

-- name: DeleteScheduleTargets :exec
DELETE FROM schedule_target st
USING schedule s
WHERE s.id = st.schedule_id
  AND s.org_id = $1
  AND st.schedule_id = $2
  AND s.deleted_at IS NULL;

-- name: LockSchedulePriority :exec
SELECT pg_advisory_xact_lock(hashtextextended('schedule_priority:' || $1::text, 0));

-- name: ListScheduleIDStatuses :many
SELECT id, status
FROM schedule
WHERE org_id = $1
  AND deleted_at IS NULL
ORDER BY priority, id;

-- name: PauseActiveSchedule :execrows
UPDATE schedule
SET status = 'paused'
WHERE org_id = $1
  AND id = $2
  AND deleted_at IS NULL
  AND status IN ('active', 'running');

-- name: ResumePausedSchedule :execrows
UPDATE schedule
SET status = $1,
    next_run_at = $2
WHERE org_id = $3
  AND id = $4
  AND deleted_at IS NULL
  AND status = 'paused';

-- name: GetScheduleTargetsByScheduleIDs :many
SELECT st.*
FROM schedule_target st
JOIN schedule s ON s.id = st.schedule_id
WHERE s.org_id = $1
  AND st.schedule_id = ANY(@schedule_ids::bigint[])
  AND s.deleted_at IS NULL;

-- name: SetScheduleRunning :execrows
UPDATE schedule
SET status = 'running'
WHERE id = $1
  AND deleted_at IS NULL
  AND status = 'active';

-- name: GetScheduleByIDForProcessor :one
SELECT * FROM schedule WHERE id = $1 AND deleted_at IS NULL;

-- name: RevertScheduleToActive :exec
UPDATE schedule SET status = 'active' WHERE id = $1 AND deleted_at IS NULL AND status = 'running';

-- name: GetActiveSchedules :many
SELECT *
FROM schedule
WHERE status IN ('active', 'running')
  AND deleted_at IS NULL
ORDER BY priority, id;

