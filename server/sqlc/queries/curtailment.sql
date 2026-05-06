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

-- name: ListCurtailmentCandidatesByOrg :many
-- Pulls per-device state for the selector's filter / rank pipeline. Returns
-- ALL devices in scope (org + optional device_identifiers narrow), including
-- unpaired / stale / unstatused — the service layer applies skip-reason
-- attribution in Go so the diagnostic detail (phantom_load vs stale vs
-- offline-residual etc.) lands in PreviewCurtailmentPlanResponse.skipped_candidates.
--
-- LEFT JOIN on telemetry: a device with no recent samples returns NULL
-- power_w / hash_rate_hs, which the service interprets as stale. The 15-min
-- window matches the design doc's staleness boundary.
--
-- device_identifiers narrow: pass NULL for whole-org scope; pass a non-empty
-- array for device-list scope (after org-ownership validation).
WITH latest_metrics AS (
    SELECT DISTINCT ON (device_metrics.device_identifier)
        device_metrics.device_identifier,
        device_metrics.time,
        device_metrics.power_w,
        device_metrics.hash_rate_hs
    FROM device_metrics
    INNER JOIN device d2 ON device_metrics.device_identifier = d2.device_identifier
        AND d2.deleted_at IS NULL
        AND d2.org_id = sqlc.arg('org_id')
    WHERE device_metrics.time > NOW() - INTERVAL '15 minutes'
    ORDER BY device_metrics.device_identifier, device_metrics.time DESC
),
latest_hourly AS (
    SELECT DISTINCT ON (device_metrics_hourly.device_identifier)
        device_metrics_hourly.device_identifier,
        device_metrics_hourly.avg_efficiency
    FROM device_metrics_hourly
    ORDER BY device_metrics_hourly.device_identifier, bucket DESC
)
SELECT
    d.device_identifier,
    dd.driver_name,
    COALESCE(dd.model, '') AS model,
    -- device_status / pairing_status default to safe sentinels when the
    -- joined row is missing. Service treats NULL device_status as
    -- "unknown" (skip with stale reason); NULL pairing_status as UNPAIRED.
    ds.status::text AS device_status,
    CASE WHEN dp.id IS NOT NULL THEN dp.pairing_status::text ELSE 'UNPAIRED' END AS pairing_status,
    lm.time            AS latest_metrics_at,
    lm.power_w         AS latest_power_w,
    lm.hash_rate_hs    AS latest_hash_rate_hs,
    lh.avg_efficiency  AS avg_efficiency
FROM device d
LEFT JOIN discovered_device dd ON dd.id = d.discovered_device_id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN latest_metrics lm ON lm.device_identifier = d.device_identifier
LEFT JOIN latest_hourly lh ON lh.device_identifier = d.device_identifier
WHERE d.org_id = sqlc.arg('org_id')
    AND d.deleted_at IS NULL
    AND (
        sqlc.narg('device_identifiers')::text[] IS NULL
        OR d.device_identifier = ANY(sqlc.narg('device_identifiers')::text[])
    );
