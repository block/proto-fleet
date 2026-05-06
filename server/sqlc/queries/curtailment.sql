-- name: GetCurtailmentOrgConfig :one
-- Read at handler entry to resolve max_duration_default_sec normalization,
-- candidate_min_power_w default (when override is not set), and the cooldown
-- window for the selector. The migration seeds one row per existing org so
-- this is guaranteed to return a row for any valid org_id.
SELECT
    org_id,
    max_duration_default_sec,
    candidate_min_power_w,
    post_event_cooldown_sec,
    created_at,
    updated_at
FROM curtailment_org_config
WHERE org_id = sqlc.arg('org_id');

-- name: ListActiveCurtailedDevicesByOrg :many
-- Devices currently locked in a non-terminal curtailment event. The selector
-- excludes these from the candidate set so a Preview cannot plan against a
-- device that another event already governs (per-device single-writer rule).
SELECT DISTINCT ct.device_identifier
FROM curtailment_target ct
JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
WHERE ce.org_id = sqlc.arg('org_id')
    AND ce.state IN ('pending', 'active', 'restoring')
    AND ct.state NOT IN ('resolved', 'restore_failed', 'released');

-- name: ListRecentlyResolvedCurtailedDevicesByOrg :many
-- Devices whose targets reached a terminal state (resolved or restore_failed)
-- within `cooldown_sec`. The selector excludes these from the candidate set
-- unless priority=EMERGENCY (cooldown bypass is enforced in Go, not in SQL).
-- The window is computed as NOW() - cooldown_sec; Postgres handles the
-- interval arithmetic so the Go layer does not need to recompute it.
SELECT DISTINCT ct.device_identifier
FROM curtailment_target ct
JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
WHERE ce.org_id = sqlc.arg('org_id')
    AND ct.state IN ('resolved', 'restore_failed')
    AND ce.ended_at IS NOT NULL
    AND ce.ended_at >= CURRENT_TIMESTAMP - (sqlc.arg('cooldown_sec')::int * INTERVAL '1 second');

-- name: InsertCurtailmentEvent :one
-- Bulk insert path used by Start (BE-3+) and by store tests in BE-2. The full
-- column list mirrors the migration so callers cannot accidentally rely on
-- DEFAULTs for values the API layer should be normalizing.
INSERT INTO curtailment_event (
    event_uuid,
    org_id,
    state,
    mode,
    strategy,
    level,
    priority,
    loop_type,
    scope_type,
    scope_jsonb,
    mode_params_jsonb,
    restore_batch_size,
    restore_batch_interval_sec,
    min_curtailed_duration_sec,
    max_duration_seconds,
    allow_unbounded,
    include_maintenance,
    force_include_maintenance,
    decision_snapshot_jsonb,
    source_actor_type,
    source_actor_id,
    external_source,
    external_reference,
    idempotency_key,
    reason,
    scheduled_start_at
) VALUES (
    sqlc.arg('event_uuid'),
    sqlc.arg('org_id'),
    sqlc.arg('state'),
    sqlc.arg('mode'),
    sqlc.arg('strategy'),
    sqlc.arg('level'),
    sqlc.arg('priority'),
    sqlc.arg('loop_type'),
    sqlc.arg('scope_type'),
    sqlc.arg('scope_jsonb'),
    sqlc.arg('mode_params_jsonb'),
    sqlc.arg('restore_batch_size'),
    sqlc.arg('restore_batch_interval_sec'),
    sqlc.arg('min_curtailed_duration_sec'),
    sqlc.narg('max_duration_seconds'),
    sqlc.arg('allow_unbounded'),
    sqlc.arg('include_maintenance'),
    sqlc.arg('force_include_maintenance'),
    sqlc.arg('decision_snapshot_jsonb'),
    sqlc.arg('source_actor_type'),
    sqlc.narg('source_actor_id'),
    sqlc.narg('external_source'),
    sqlc.narg('external_reference'),
    sqlc.narg('idempotency_key'),
    sqlc.arg('reason'),
    sqlc.narg('scheduled_start_at')
)
RETURNING id, event_uuid, created_at, updated_at;

-- name: GetCurtailmentEventByUUID :one
-- Org-scoped read; callers MUST pass the caller's org_id to prevent cross-tenant
-- snapshot exposure. Used by BE-2 store tests to verify migration constraints
-- round-trip correctly.
SELECT *
FROM curtailment_event
WHERE event_uuid = sqlc.arg('event_uuid')
    AND org_id = sqlc.arg('org_id');

-- name: InsertCurtailmentTarget :exec
-- Per-target row insert. BE-3 dispatch path inserts these in a single
-- transaction with the parent event row; BE-2 store tests use it to round-trip
-- schema constraints.
INSERT INTO curtailment_target (
    curtailment_event_id,
    device_identifier,
    target_type,
    state,
    desired_state,
    baseline_power_w,
    selector_rationale_jsonb
) VALUES (
    sqlc.arg('curtailment_event_id'),
    sqlc.arg('device_identifier'),
    sqlc.arg('target_type'),
    sqlc.arg('state'),
    sqlc.arg('desired_state'),
    sqlc.narg('baseline_power_w'),
    sqlc.narg('selector_rationale_jsonb')
);

-- name: ListCurtailmentTargetsByEvent :many
-- Org-scoped via the join. Used by BE-2 store tests and by future Get/List
-- read paths that need to surface the per-event target rollup.
SELECT ct.*
FROM curtailment_target ct
JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
WHERE ce.org_id = sqlc.arg('org_id')
    AND ce.event_uuid = sqlc.arg('event_uuid')
ORDER BY ct.device_identifier;

-- name: GetCurtailmentReconcilerHeartbeat :one
SELECT id, last_tick_at, last_tick_uuid, last_tick_duration_ms, active_event_count
FROM curtailment_reconciler_heartbeat
WHERE id = 1;
