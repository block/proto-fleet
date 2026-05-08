-- name: GetCurtailmentOrgConfig :one
-- Per-org tunables: max-duration default, candidate-power floor, cooldown
-- window. Existence guaranteed: migration seeds existing orgs;
-- EnsureCurtailmentOrgConfig backfills post-migration tenants.
SELECT
    org_id,
    max_duration_default_sec,
    candidate_min_power_w,
    post_event_cooldown_sec,
    created_at,
    updated_at
FROM curtailment_org_config
WHERE org_id = sqlc.arg('org_id');

-- name: EnsureCurtailmentOrgConfig :one
-- Idempotent backfill: INSERT ... DO NOTHING preserves updated_at as a
-- config-change signal; fallback SELECT returns the existing row. Both
-- branches join `active` (organization.deleted_at IS NULL) so soft-deleted
-- orgs return zero rows; caller maps to NotFound.
WITH active AS (
    SELECT id
    FROM organization
    WHERE id = sqlc.arg('org_id')
        AND deleted_at IS NULL
),
ins AS (
    INSERT INTO curtailment_org_config (org_id)
    SELECT id FROM active
    ON CONFLICT (org_id) DO NOTHING
    RETURNING
        org_id,
        max_duration_default_sec,
        candidate_min_power_w,
        post_event_cooldown_sec,
        created_at,
        updated_at
)
SELECT
    org_id,
    max_duration_default_sec,
    candidate_min_power_w,
    post_event_cooldown_sec,
    created_at,
    updated_at
FROM ins
UNION ALL
SELECT
    c.org_id,
    c.max_duration_default_sec,
    c.candidate_min_power_w,
    c.post_event_cooldown_sec,
    c.created_at,
    c.updated_at
FROM curtailment_org_config c
INNER JOIN active a ON a.id = c.org_id
WHERE NOT EXISTS (SELECT 1 FROM ins)
LIMIT 1;

-- name: ListActiveCurtailedDevicesByOrg :many
-- Devices locked in a non-terminal event; excluded from candidates to
-- enforce the per-device single-writer rule.
SELECT DISTINCT ct.device_identifier
FROM curtailment_target ct
JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
WHERE ce.org_id = sqlc.arg('org_id')
    AND ce.state IN ('pending', 'active', 'restoring')
    AND ct.state NOT IN ('resolved', 'restore_failed', 'released');

-- name: ListRecentlyResolvedCurtailedDevicesByOrg :many
-- Targets that hit a terminal state within `cooldown_sec`. Selector
-- excludes these unless priority=EMERGENCY (Go-side bypass).
SELECT DISTINCT ct.device_identifier
FROM curtailment_target ct
JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
WHERE ce.org_id = sqlc.arg('org_id')
    AND ct.state IN ('resolved', 'restore_failed')
    AND ce.ended_at IS NOT NULL
    AND ce.ended_at >= CURRENT_TIMESTAMP - (sqlc.arg('cooldown_sec')::int * INTERVAL '1 second');

-- name: InsertCurtailmentEvent :one
-- Full column list mirrors the migration so callers can't rely on DEFAULTs
-- for values the API layer should be normalizing.
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
    scheduled_start_at,
    created_by_user_id
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
    sqlc.narg('scheduled_start_at'),
    sqlc.arg('created_by_user_id')
)
RETURNING id, event_uuid, created_at, updated_at;

-- name: GetCurtailmentEventByUUID :one
-- Org-scoped: callers MUST pass org_id to prevent cross-tenant exposure.
SELECT *
FROM curtailment_event
WHERE event_uuid = sqlc.arg('event_uuid')
    AND org_id = sqlc.arg('org_id');

-- name: InsertCurtailmentTarget :exec
-- Start dispatch inserts these in the event-row transaction.
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
-- Org-scoped via the join.
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

-- name: ListNonTerminalCurtailmentEvents :many
-- System-scope (no org filter); reconciler is a singleton driving all orgs.
-- Order by id keeps per-tick processing deterministic.
SELECT *
FROM curtailment_event
WHERE state IN ('pending', 'active', 'restoring')
ORDER BY id;

-- name: UpdateCurtailmentEventState :exec
-- COALESCE: nil narg leaves started_at/ended_at unchanged. Timestamps are
-- write-once, so the OR-NULL pattern is fine.
UPDATE curtailment_event
SET state      = sqlc.arg('state'),
    started_at = COALESCE(sqlc.narg('started_at'), started_at),
    ended_at   = COALESCE(sqlc.narg('ended_at'),   ended_at)
WHERE id = sqlc.arg('id');

-- name: UpdateCurtailmentTargetState :exec
-- Reconciler patch: COALESCE preserves un-supplied columns so partial
-- updates don't clobber values from earlier ticks. retry_count is
-- read-then-written inside the tick.
UPDATE curtailment_target
SET state              = sqlc.arg('state'),
    last_dispatched_at = COALESCE(sqlc.narg('last_dispatched_at'), last_dispatched_at),
    last_batch_uuid    = COALESCE(sqlc.narg('last_batch_uuid'),    last_batch_uuid),
    observed_power_w   = COALESCE(sqlc.narg('observed_power_w'),   observed_power_w),
    observed_at        = COALESCE(sqlc.narg('observed_at'),        observed_at),
    confirmed_at       = COALESCE(sqlc.narg('confirmed_at'),       confirmed_at),
    retry_count        = COALESCE(sqlc.narg('retry_count'),        retry_count),
    last_error         = COALESCE(sqlc.narg('last_error'),         last_error)
WHERE curtailment_event_id = sqlc.arg('curtailment_event_id')
  AND device_identifier    = sqlc.arg('device_identifier');

-- name: UpsertCurtailmentReconcilerHeartbeat :exec
-- Singleton row at id=1 (CHECK + PK enforce it). INSERT path only fires if
-- the seeded row is manually deleted.
INSERT INTO curtailment_reconciler_heartbeat (id, last_tick_at, last_tick_uuid, last_tick_duration_ms, active_event_count)
VALUES (1, sqlc.arg('last_tick_at'), sqlc.arg('last_tick_uuid'), sqlc.narg('last_tick_duration_ms'), sqlc.arg('active_event_count'))
ON CONFLICT (id) DO UPDATE
SET last_tick_at          = EXCLUDED.last_tick_at,
    last_tick_uuid        = EXCLUDED.last_tick_uuid,
    last_tick_duration_ms = EXCLUDED.last_tick_duration_ms,
    active_event_count    = EXCLUDED.active_event_count;

-- name: ListCurtailmentCandidatesByOrg :many
-- Per-device state for the selector. Returns every in-scope device
-- (unpaired/stale/unstatused included); service applies skip-reason
-- attribution. LEFT JOIN telemetry: nil power/hash means stale (15-min
-- window). device_identifiers: nil = whole-org; non-empty for device-list
-- scope (post org-ownership check).
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
    INNER JOIN device d3 ON device_metrics_hourly.device_identifier = d3.device_identifier
        AND d3.deleted_at IS NULL
        AND d3.org_id = sqlc.arg('org_id')
    -- 24h window covers TimescaleDB end-offset + operator-timezone gaps.
    WHERE device_metrics_hourly.bucket > NOW() - INTERVAL '24 hours'
    ORDER BY device_metrics_hourly.device_identifier, bucket DESC
)
SELECT
    d.device_identifier,
    dd.driver_name,
    COALESCE(dd.model, '') AS model,
    -- COALESCE: sqlc generates non-nullable string; empty-string is the
    -- "unknown status" sentinel the service treats as stale. NULL
    -- pairing_status normalizes to UNPAIRED below.
    COALESCE(ds.status::text, ''::text)::text AS device_status,
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
    )
-- Stable order: makes the selector's stable sort deterministic when
-- avg_efficiency ties or is NULL.
ORDER BY d.device_identifier;
