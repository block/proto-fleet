-- name: GetSchedule :one
SELECT *
FROM schedule
WHERE org_id = $1
  AND id = $2
  AND deleted_at IS NULL;

-- name: ListSchedules :many
SELECT *
FROM schedule
WHERE org_id = $1
  AND deleted_at IS NULL
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action'))
ORDER BY priority, id;

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

-- name: UpdateSchedule :exec
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
    next_run_at   = $11
WHERE org_id = $12
  AND id = $13
  AND deleted_at IS NULL;

-- name: SoftDeleteSchedule :exec
UPDATE schedule
SET deleted_at = CURRENT_TIMESTAMP
WHERE org_id = $1
  AND id = $2
  AND deleted_at IS NULL;

-- name: UpdateScheduleStatus :exec
UPDATE schedule
SET status = $1
WHERE org_id = $2
  AND id = $3
  AND deleted_at IS NULL;

-- name: ReorderSchedules :exec
WITH new_order AS (
    SELECT id, ordinality::int AS new_priority
    FROM unnest(@ids::bigint[]) WITH ORDINALITY AS u(id, ordinality)
),
clear AS (
    UPDATE schedule s
    SET priority = -t.new_priority
    FROM new_order t
    WHERE s.id = t.id
      AND s.org_id = $1
      AND s.deleted_at IS NULL
)
UPDATE schedule s
SET priority = t.new_priority
FROM new_order t
WHERE s.id = t.id
  AND s.org_id = $1
  AND s.deleted_at IS NULL;

-- name: GetDueSchedules :many
SELECT *
FROM schedule
WHERE next_run_at <= NOW()
  AND status = 'active'
  AND deleted_at IS NULL
ORDER BY priority, id;

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

-- name: GetRunningPowerTargetSchedules :many
SELECT *
FROM schedule
WHERE org_id = $1
  AND status = 'running'
  AND action = 'set_power_target'
  AND deleted_at IS NULL
ORDER BY priority, id;

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
