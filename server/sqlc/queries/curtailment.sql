-- name: GetCurtailmentOrgConfig :one
-- Per-org tunables. Migration seeds existing orgs;
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
-- Idempotent backfill (INSERT ... DO NOTHING + fallback SELECT). Both
-- branches require organization.deleted_at IS NULL.
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
-- Restored/failed targets the selector excludes (unless priority=EMERGENCY,
-- Go-side bypass): a target restored mid-stagger stays protected while its
-- event is still non-terminal (the old org-level singleton enforced this
-- implicitly), then for `cooldown_sec` after the event ends.
SELECT DISTINCT ct.device_identifier
FROM curtailment_target ct
JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
WHERE ce.org_id = sqlc.arg('org_id')
    AND ct.state IN ('resolved', 'restore_failed')
    AND (
        ce.state IN ('pending', 'active', 'restoring')
        OR ce.ended_at >= CURRENT_TIMESTAMP - (sqlc.arg('cooldown_sec')::int * INTERVAL '1 second')
    );

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
    ended_at,
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
    sqlc.narg('ended_at'),
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
-- Idempotent replay lookup; state filter mirrors the partial unique
-- index so a retry of a long-completed event is treated as a fresh Start.
SELECT *
FROM curtailment_event
WHERE org_id = sqlc.arg('org_id')
    AND idempotency_key = sqlc.arg('idempotency_key')
    AND state IN ('pending', 'active', 'restoring')
LIMIT 1;

-- name: GetCurtailmentEventByExternalReference :one
-- Webhook idempotent replay lookup; mirrors the
-- uq_curtailment_event_external_ref partial index.
SELECT *
FROM curtailment_event
WHERE org_id = sqlc.arg('org_id')
    AND external_source = sqlc.arg('external_source')
    AND external_reference = sqlc.arg('external_reference')
    AND state IN ('pending', 'active', 'restoring')
LIMIT 1;

-- name: CurtailmentEventHasInFlightTargets :one
-- AdminTerminate's Stop-first gate: true when any target still has an
-- in-flight Curtail (desired_state='curtailed' + non-terminal state).
-- DISPATCHING inclusion closes the race against a tick mid-dispatch; the
-- desired_state scope avoids blocking AdminTerminate on RESTORING events
-- whose in-flight commands are Uncurtails.
SELECT EXISTS (
    SELECT 1
    FROM curtailment_target
    WHERE curtailment_event_id = sqlc.arg('curtailment_event_id')
        AND desired_state = 'curtailed'
        AND state IN ('dispatching', 'dispatched', 'confirmed', 'drifted')
) AS has_in_flight;

-- name: AdminTerminateCurtailmentEvent :one
-- Flips pending/restoring → target_state (CANCELLED or FAILED).
-- Locks the event row before evaluating the in-flight target predicate so
-- reconciler target claims (which lock the same parent row) serialize with
-- forced termination. Zero-row return lets the caller route active,
-- in-flight, and already-terminal cases.
WITH locked_event AS MATERIALIZED (
    SELECT id
    FROM curtailment_event
    WHERE curtailment_event.id = sqlc.arg('id')
        AND curtailment_event.org_id = sqlc.arg('org_id')
        AND curtailment_event.state IN ('pending', 'restoring')
    FOR UPDATE
)
UPDATE curtailment_event
SET state      = sqlc.arg('target_state')::TEXT,
    ended_at   = NOW(),
    updated_at = NOW()
FROM locked_event
WHERE curtailment_event.id = locked_event.id
    AND NOT EXISTS (
        SELECT 1
        FROM curtailment_target
        WHERE curtailment_event_id = locked_event.id
            AND desired_state = 'curtailed'
            AND state IN ('dispatching', 'dispatched', 'confirmed', 'drifted')
    )
RETURNING curtailment_event.*;

-- name: SweepCurtailmentTargetsToRestoreFailed :exec
-- Force every non-terminal target → RESTORE_FAILED with the
-- admin-terminate reason. Paired with AdminTerminateCurtailmentEvent
-- in one tx. (curtailment_target has no updated_at column — row
-- mutability rides on state + per-write timestamps.)
UPDATE curtailment_target
SET state      = 'restore_failed',
    last_error = sqlc.arg('last_error')::TEXT
WHERE curtailment_event_id = sqlc.arg('curtailment_event_id')
    AND state NOT IN ('resolved', 'restore_failed', 'released');

-- name: UpdateCurtailmentEventOperatorFields :one
-- Partial update; nil params COALESCE-preserve. State filter is the
-- race-loss guard — zero rows means the event advanced between the
-- service's pre-read and this UPDATE.
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
-- Cursor-paginated history (newest-first). cursor_id=0 is the first page;
-- state_filters empty = all states; caller passes limit+1 to detect a next page.
--
-- decision_snapshot_jsonb is projected with the per-device `skipped` array
-- stripped into a `skipped_aggregate` reason→count map so 10K-miner
-- snapshots don't ride the wire on every list row.
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
    AND (
        COALESCE(cardinality(sqlc.arg('state_filters')::TEXT[]), 0) = 0
        OR state = ANY(sqlc.arg('state_filters')::TEXT[])
    )
ORDER BY id DESC
LIMIT sqlc.arg('row_limit')::BIGINT;

-- name: GetActiveCurtailmentEvent :one
-- Most-recent non-terminal event for the org (several can coexist, one per
-- disjoint device scope). Ordered by effective time — created_at for pending
-- events so a fresh pending isn't buried behind older active ones — id tiebreak.
SELECT *
FROM curtailment_event
WHERE org_id = sqlc.arg('org_id')
    AND state IN ('pending', 'active', 'restoring')
ORDER BY COALESCE(started_at, created_at) DESC, id DESC
LIMIT 1;

-- name: ListActiveCurtailmentEvents :many
-- Org-scoped list of every non-terminal event. Multiple can be active when
-- they target disjoint device scopes (e.g. per-site curtailment). Most-recent
-- first by effective time (started_at, or created_at for pending), id tiebreak.
SELECT *
FROM curtailment_event
WHERE org_id = sqlc.arg('org_id')
    AND state IN ('pending', 'active', 'restoring')
ORDER BY COALESCE(started_at, created_at) DESC, id DESC;

-- name: BulkInsertCurtailmentTargets :execrows
-- Bulk fan-out via jsonb_to_recordset: per-row fields ride in a JSONB
-- payload, missing/null keys map to SQL NULL. :execrows lets the caller
-- pin (rows == len(input)) to detect partial writes.
INSERT INTO curtailment_target (
    curtailment_event_id,
    device_identifier,
    target_type,
    state,
    desired_state,
    baseline_power_w,
    selector_rationale_jsonb
)
SELECT
    sqlc.arg('curtailment_event_id'),
    t.device_identifier,
    t.target_type,
    t.state,
    t.desired_state,
    t.baseline_power_w,
    t.selector_rationale_jsonb
FROM jsonb_to_recordset(sqlc.arg('targets_jsonb')::JSONB) AS t(
    device_identifier         TEXT,
    target_type               TEXT,
    state                     TEXT,
    desired_state             TEXT,
    baseline_power_w          NUMERIC(12,3),
    selector_rationale_jsonb  JSONB
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
-- Row-count return is the race-loss guard; expected_state prevents stale
-- phase writes (e.g. pending→active after Stop moved the event to restoring).
-- nil narg preserves timestamps via COALESCE.
UPDATE curtailment_event
SET state      = sqlc.arg('state'),
    started_at = COALESCE(sqlc.narg('started_at'), started_at),
    ended_at   = COALESCE(sqlc.narg('ended_at'),   ended_at)
WHERE id = sqlc.arg('id')
  AND state = sqlc.arg('expected_state')
  AND state IN ('pending', 'active', 'restoring');

-- name: BeginCurtailmentRestoration :one
-- Stop's event-side flip to 'restoring'. The WHERE state-guard is the
-- concurrency control; the loser sees zero rows and the store re-reads
-- to distinguish "already restoring" from "already terminal."
UPDATE curtailment_event
SET state = 'restoring'
WHERE id = sqlc.arg('id')
  AND state IN ('pending', 'active')
RETURNING *;

-- name: ResetCurtailmentTargetsForRestore :exec
-- Stop's target-side write; flips non-terminal targets to
-- desired_state='active' and clears phase-local cursors so the restorer
-- has an unambiguous queue. Terminal states are untouched.
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

-- name: ResumeCurtailmentFromRestoring :one
-- Restore reversal: go back through pending so the curtail dispatcher picks
-- up reset targets.
UPDATE curtailment_event
SET state = 'pending'
WHERE id = sqlc.arg('id')
  AND state = 'restoring'
RETURNING *;

-- name: ResetCurtailmentTargetsForRecurtail :one
-- Reopen restore targets for curtailment. Counts let the store reject partial
-- resets when another non-terminal event already has unresolved work for one
-- of the same devices.
WITH recurtail_candidates AS MATERIALIZED (
    SELECT ct.curtailment_event_id, ct.device_identifier
    FROM curtailment_target AS ct
    WHERE ct.curtailment_event_id = sqlc.arg('curtailment_event_id')
      AND ct.desired_state = 'active'
      AND ct.state <> 'released'
),
reset_targets AS (
    UPDATE curtailment_target AS target
    SET desired_state      = 'curtailed',
        state              = 'pending',
        retry_count        = 0,
        last_dispatched_at = NULL,
        last_batch_uuid    = NULL,
        confirmed_at       = NULL,
        last_error         = NULL
    FROM recurtail_candidates candidate
    WHERE target.curtailment_event_id = candidate.curtailment_event_id
      AND target.device_identifier = candidate.device_identifier
      AND NOT EXISTS (
          SELECT 1
          FROM curtailment_target other_target
          JOIN curtailment_event other_event
              ON other_event.id = other_target.curtailment_event_id
          WHERE other_target.device_identifier = candidate.device_identifier
            AND other_target.curtailment_event_id <> sqlc.arg('curtailment_event_id')
            AND other_event.state IN ('pending', 'active', 'restoring')
            AND other_target.state NOT IN ('resolved', 'restore_failed', 'released')
      )
    RETURNING 1
)
SELECT
    (SELECT count(*) FROM recurtail_candidates)::BIGINT AS target_count,
    (SELECT count(*) FROM reset_targets)::BIGINT AS reset_count;

-- name: UpdateCurtailmentTargetState :execrows
-- Reconciler patch. COALESCE preserves un-supplied columns; empty
-- last_error is the explicit clear sentinel that maps to SQL NULL.
--
-- Parent event row is locked before the target update so Stop/AdminTerminate
-- and target claims serialize on the event lifecycle. expected_event_state
-- maps stale phase writes to ErrCurtailmentEventStateRaceLoss.
--
-- expected_desired_state scopes the write to one dispatch direction so
-- a concurrent Stop's reset isn't clobbered by a Curtail-phase post-cmd
-- write (observeRestoring picks up the reset target afterwards).
WITH locked_event AS MATERIALIZED (
    SELECT id
    FROM curtailment_event
    WHERE id = sqlc.arg('curtailment_event_id')
        AND state IN ('pending', 'active', 'restoring')
        AND (sqlc.narg('expected_event_state')::TEXT IS NULL
             OR state = sqlc.narg('expected_event_state')::TEXT)
    FOR UPDATE
)
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
FROM locked_event
WHERE curtailment_event_id = locked_event.id
  AND device_identifier    = sqlc.arg('device_identifier')
  AND (sqlc.narg('expected_desired_state')::text IS NULL
       OR desired_state = sqlc.narg('expected_desired_state')::text);

-- name: BumpCurtailmentTargetRetry :execrows
-- Fallback when UpdateCurtailmentTargetState fails non-race-loss:
-- advance retry_count alone so MaxRetries → RESTORE_FAILED escalation
-- still lands on the next successful state-change write. EXISTS guard
-- → zero rows → ErrCurtailmentEventStateRaceLoss on terminal parent.
UPDATE curtailment_target
SET retry_count = retry_count + 1
WHERE curtailment_event_id = sqlc.arg('curtailment_event_id')
  AND device_identifier    = sqlc.arg('device_identifier')
  AND EXISTS (
      SELECT 1
      FROM curtailment_event
      WHERE id = sqlc.arg('curtailment_event_id')
        AND state IN ('pending', 'active', 'restoring')
  );

-- name: UpsertCurtailmentReconcilerHeartbeat :exec
-- Singleton row at id=1; INSERT path only fires on accidental deletion.
INSERT INTO curtailment_reconciler_heartbeat (id, last_tick_at, last_tick_uuid, last_tick_duration_ms, active_event_count)
VALUES (1, sqlc.arg('last_tick_at'), sqlc.arg('last_tick_uuid'), sqlc.narg('last_tick_duration_ms'), sqlc.arg('active_event_count'))
ON CONFLICT (id) DO UPDATE
SET last_tick_at          = EXCLUDED.last_tick_at,
    last_tick_uuid        = EXCLUDED.last_tick_uuid,
    last_tick_duration_ms = EXCLUDED.last_tick_duration_ms,
    active_event_count    = EXCLUDED.active_event_count;

-- name: ListCurtailmentCandidatesByOrg :many
-- Per-device state for the selector. Returns every in-scope device;
-- service applies skip-reason attribution. nil power/hash = stale
-- (15-min window). device_identifiers nil = whole-org.
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
-- Stable order so the selector's stable sort is deterministic on ties.
ORDER BY d.device_identifier;
