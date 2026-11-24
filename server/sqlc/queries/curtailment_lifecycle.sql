-- name: LockCurtailmentEventLifecycle :exec
-- Serialize lifecycle changes with physical fan I/O without retaining an event row lock.
SELECT pg_advisory_xact_lock(hashtextextended('curtailment-lifecycle:' || sqlc.arg('event_id')::text, 0));

-- name: LockCurtailmentEventLifecycleByUUID :exec
SELECT pg_advisory_xact_lock(hashtextextended('curtailment-lifecycle:' || id::text, 0))
FROM curtailment_event WHERE event_uuid = sqlc.arg('event_uuid') AND org_id = sqlc.arg('org_id');

-- name: LockCurtailmentFanSession :exec
SELECT pg_advisory_lock(hashtextextended('curtailment-fan:' || sqlc.arg('fan_id')::text, 0));

-- name: UnlockCurtailmentFanSession :exec
SELECT pg_advisory_unlock(hashtextextended('curtailment-fan:' || sqlc.arg('fan_id')::text, 0));

-- name: LockCurtailmentLifecycleSession :exec
SELECT pg_advisory_lock(hashtextextended('curtailment-lifecycle:' || sqlc.arg('event_id')::text, 0));

-- name: UnlockCurtailmentLifecycleSession :exec
SELECT pg_advisory_unlock(hashtextextended('curtailment-lifecycle:' || sqlc.arg('event_id')::text, 0));

-- name: GetCurtailmentEventForFanOperation :one
SELECT * FROM curtailment_event WHERE id = sqlc.arg('id');
