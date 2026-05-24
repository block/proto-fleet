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
    created_by_user_id,
    effective_batch_size
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
    sqlc.arg('created_by_user_id'),
    sqlc.arg('effective_batch_size')
)
RETURNING id, event_uuid, created_at, updated_at;

-- name: GetCurtailmentEventByUUID :one
-- Org-scoped: callers MUST pass org_id to prevent cross-tenant exposure.
SELECT *
FROM curtailment_event
WHERE event_uuid = sqlc.arg('event_uuid')
    AND org_id = sqlc.arg('org_id');

-- name: GetCurtailmentEventByIdempotencyKey :one
-- Idempotent replay lookup. Returns zero rows when no prior call used the
-- key. Backed by partial unique index uq_curtailment_event_idempotency.
SELECT *
FROM curtailment_event
WHERE org_id = sqlc.arg('org_id')
    AND idempotency_key = sqlc.arg('idempotency_key')
LIMIT 1;

-- name: GetCurtailmentEventByExternalReference :one
-- Webhook-style idempotent replay lookup. Returns zero rows when no prior
-- call carried the same (source, reference). Backed by partial unique
-- index uq_curtailment_event_external_ref.
SELECT *
FROM curtailment_event
WHERE org_id = sqlc.arg('org_id')
    AND external_source = sqlc.arg('external_source')
    AND external_reference = sqlc.arg('external_reference')
LIMIT 1;

-- name: CurtailmentEventHasInFlightTargets :one
-- True if any target on the event is in flight — i.e., the reconciler
-- has written DISPATCHING (about to issue a command), DISPATCHED (command
-- enqueued, awaiting telemetry), CONFIRMED (telemetry verified), or
-- DRIFTED (re-dispatching). Used as the admin-terminate precondition so
-- a concurrent terminate cannot fire while a tick is mid-dispatch.
-- DISPATCHING is the load-bearing inclusion: the reconciler writes
-- DISPATCHING *before* calling cmd.Curtail, so a terminate that races the
-- command observes it and rejects as Stop-first instead of letting the
-- command land against a sweep-already-committed event with no
-- compensating Uncurtail.
SELECT EXISTS (
    SELECT 1
    FROM curtailment_target
    WHERE curtailment_event_id = sqlc.arg('curtailment_event_id')
        AND state IN ('dispatching', 'dispatched', 'confirmed', 'drifted')
) AS has_in_flight;

-- name: AdminTerminateCurtailmentEvent :one
-- Forces a pending/restoring event to the operator-chosen terminal target_state
-- (validated CANCELLED or FAILED at the service boundary). Returns zero rows
-- when the event is active/already terminal so the caller can route by current
-- state: active requires StopCurtailment first, terminal is idempotent no-op
-- when the target matches or FailedPrecondition when different. Ended_at and
-- updated_at advance on a successful transition.
UPDATE curtailment_event
SET state      = sqlc.arg('target_state')::TEXT,
    ended_at   = NOW(),
    updated_at = NOW()
WHERE id = sqlc.arg('id')
    AND org_id = sqlc.arg('org_id')
    AND state IN ('pending', 'restoring')
RETURNING *;

-- name: SweepCurtailmentTargetsToRestoreFailed :exec
-- Forces every non-terminal target on the event to RESTORE_FAILED. Paired
-- with AdminTerminateCurtailmentEvent inside a single transaction so the
-- per-device suppression filter releases its hold the moment the event row
-- flips terminal. last_error carries the admin-terminate reason for audit.
UPDATE curtailment_target
SET state      = 'restore_failed',
    last_error = sqlc.arg('last_error')::TEXT,
    updated_at = NOW()
WHERE curtailment_event_id = sqlc.arg('curtailment_event_id')
    AND state NOT IN ('resolved', 'restore_failed', 'released');

-- name: UpdateCurtailmentEventOperatorFields :one
-- Partial update of operator-safe fields. nil params COALESCE-preserve
-- existing values. The state filter is defense-in-depth: the service
-- pre-reads the row to surface a clean FailedPrecondition message, so a
-- zero-row return here is the race-loss path (state advanced between the
-- pre-read and this UPDATE) and the caller maps it to FailedPrecondition.
UPDATE curtailment_event
SET reason                     = COALESCE(sqlc.narg('reason')::TEXT, reason),
    restore_batch_size         = COALESCE(sqlc.narg('restore_batch_size')::INT, restore_batch_size),
    restore_batch_interval_sec = COALESCE(sqlc.narg('restore_batch_interval_sec')::INT, restore_batch_interval_sec),
    max_duration_seconds       = COALESCE(sqlc.narg('max_duration_seconds')::INT, max_duration_seconds),
    updated_at                 = NOW()
WHERE id = sqlc.arg('id')
    AND org_id = sqlc.arg('org_id')
    AND state IN ('pending', 'active')
RETURNING *;

-- name: ListCurtailmentEventsForOrg :many
-- Cursor-paginated history, ordered newest-first by id. cursor_id=0 reads
-- the first page; subsequent pages pass the last id from the previous page.
-- state_filter is empty for "all states" or one of the event-state values
-- to filter on. Caller passes limit+1 so the result indicates a next page
-- when the slice exceeds the requested page size.
--
-- decision_snapshot_jsonb is projected with the per-device `skipped` array
-- stripped at the SQL boundary so a 10K-miner event's multi-MB skip list
-- doesn't ride the wire for every list row. The aggregate is computed before
-- stripping so production list rows match the documented read-API shape.
-- The win is on the application tier (network + JSON decode); Postgres still
-- TOAST-detoasts the full column once per row to evaluate jsonb_typeof and
-- the aggregate subquery, so the database-side I/O cost is unchanged.
SELECT
    id, event_uuid, org_id, state, mode, strategy, level, priority,
    loop_type, scope_type, scope_jsonb, mode_params_jsonb,
    restore_batch_size, restore_batch_interval_sec, effective_batch_size,
    min_curtailed_duration_sec, max_duration_seconds, allow_unbounded,
    include_maintenance, force_include_maintenance,
    CASE
        WHEN jsonb_typeof(decision_snapshot_jsonb->'skipped') = 'array' THEN
            jsonb_set(
                decision_snapshot_jsonb - 'skipped',
                '{skipped_aggregate}',
                COALESCE(
                    (
                        SELECT jsonb_object_agg(reason, skipped_count)
                        FROM (
                            SELECT skipped_entry->>'reason' AS reason, count(*) AS skipped_count
                            FROM jsonb_array_elements(decision_snapshot_jsonb->'skipped') AS skipped_entry
                            WHERE skipped_entry->>'reason' <> ''
                            GROUP BY skipped_entry->>'reason'
                        ) skipped_counts
                    ),
                    '{}'::JSONB
                ),
                true
            )
        ELSE decision_snapshot_jsonb
    END::JSONB AS decision_snapshot_jsonb,
    source_actor_type, source_actor_id,
    external_source, external_reference, idempotency_key,
    supersedes_event_id, reason, scheduled_start_at, started_at, ended_at,
    created_at, updated_at, created_by_user_id
FROM curtailment_event
WHERE org_id = sqlc.arg('org_id')
    AND (sqlc.arg('cursor_id')::BIGINT = 0 OR id < sqlc.arg('cursor_id')::BIGINT)
    AND (sqlc.arg('state_filter')::TEXT = '' OR state = sqlc.arg('state_filter')::TEXT)
ORDER BY id DESC
LIMIT sqlc.arg('row_limit')::BIGINT;

-- name: GetActiveCurtailmentEvent :one
-- Org-scoped recovery path for pending/active/restoring events. At most one
-- row matches per org under uq_curtailment_event_one_non_terminal_per_org;
-- LIMIT 1 with no ORDER BY lets the planner satisfy the lookup via the
-- partial unique index without a sort step.
SELECT *
FROM curtailment_event
WHERE org_id = sqlc.arg('org_id')
    AND state IN ('pending', 'active', 'restoring')
LIMIT 1;

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

-- name: UpdateCurtailmentEventState :execrows
-- COALESCE: nil narg leaves started_at/ended_at unchanged. Timestamps are
-- write-once, so the OR-NULL pattern is fine. The row-count return is
-- load-bearing: 0 rows affected means the event advanced out of the
-- non-terminal set between the reconciler's snapshot and this write (a
-- concurrent Stop/AdminTerminate), and the store maps that to the typed
-- ErrCurtailmentEventStateRaceLoss so the caller can log/metric without
-- treating it as an internal error.
UPDATE curtailment_event
SET state      = sqlc.arg('state'),
    started_at = COALESCE(sqlc.narg('started_at'), started_at),
    ended_at   = COALESCE(sqlc.narg('ended_at'),   ended_at)
WHERE id = sqlc.arg('id')
  AND state IN ('pending', 'active', 'restoring');

-- name: BeginCurtailmentRestoration :one
-- Stop's event-side write: flips state to 'restoring'. effective_batch_size
-- was stamped at Start (computed from the selected target count), so this
-- query only transitions state. The WHERE state-guard is the load-bearing
-- concurrency control: concurrent Stop calls race on the same row's per-row
-- write lock; the loser sees zero rows updated and the store's ErrNoRows
-- re-read distinguishes "already restoring" from "already terminal".
-- RETURNING shape mirrors GetCurtailmentEventByUUID.
UPDATE curtailment_event
SET state = 'restoring'
WHERE id = sqlc.arg('id')
  AND state IN ('pending', 'active')
RETURNING *;

-- name: ResetCurtailmentTargetsForRestore :exec
-- Stop's target-side write inside the same tx as BeginCurtailmentRestoration.
-- Non-terminal targets flip to desired_state='active' (restore phase) and
-- their phase-local cursors reset so the restorer has an unambiguous queue
-- after a fleetd restart. Terminal states are untouched (resolved /
-- restore_failed / released keep their meaning across the phase change).
UPDATE curtailment_target
SET desired_state      = 'active',
    state              = 'pending',
    retry_count        = 0,
    last_dispatched_at = NULL,
    last_batch_uuid    = NULL,
    confirmed_at       = NULL,
    last_error         = NULL
WHERE curtailment_event_id = sqlc.arg('curtailment_event_id')
  AND state NOT IN ('resolved', 'restore_failed', 'released');

-- name: UpdateCurtailmentTargetState :execrows
-- Reconciler patch: COALESCE preserves un-supplied columns so partial
-- updates don't clobber values from earlier ticks. retry_count is
-- read-then-written inside the tick. An empty last_error string is an
-- explicit clear signal from successful redispatch paths and maps to SQL NULL.
--
-- The EXISTS guard silently no-ops the UPDATE when the parent event has
-- gone terminal (concurrent Stop/AdminTerminate landed). :execrows lets
-- the store map zero rows to ErrCurtailmentEventStateRaceLoss so the
-- reconciler can log + meter the signal rather than treating the silent
-- skip as success. The in-memory mirror update is gated on a clean
-- return; the sentinel keeps the mirror in sync with the persisted state.
UPDATE curtailment_target
SET state              = sqlc.arg('state'),
    last_dispatched_at = COALESCE(sqlc.narg('last_dispatched_at'), last_dispatched_at),
    last_batch_uuid    = COALESCE(sqlc.narg('last_batch_uuid'),    last_batch_uuid),
    observed_power_w   = COALESCE(sqlc.narg('observed_power_w'),   observed_power_w),
    observed_at        = COALESCE(sqlc.narg('observed_at'),        observed_at),
    confirmed_at       = COALESCE(sqlc.narg('confirmed_at'),       confirmed_at),
    retry_count        = COALESCE(sqlc.narg('retry_count'),        retry_count),
    last_error         = CASE
        WHEN sqlc.narg('last_error')::text IS NULL THEN last_error
        ELSE NULLIF(sqlc.narg('last_error')::text, '')
    END
WHERE curtailment_event_id = sqlc.arg('curtailment_event_id')
  AND device_identifier    = sqlc.arg('device_identifier')
  AND EXISTS (
      SELECT 1
      FROM curtailment_event
      WHERE curtailment_event.id = sqlc.arg('curtailment_event_id')
        AND curtailment_event.state IN ('pending', 'active', 'restoring')
  );

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
