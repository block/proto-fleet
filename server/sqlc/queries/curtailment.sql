-- name: GetCurtailmentOrgConfig :one
-- Per-org tunables. Migration seeds existing orgs;
-- EnsureCurtailmentOrgConfig backfills post-migration tenants.
SELECT
    org_id,
    max_duration_default_sec,
    candidate_min_power_w,
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
        created_at,
        updated_at
)
SELECT
    org_id,
    max_duration_default_sec,
    candidate_min_power_w,
    created_at,
    updated_at
FROM ins
UNION ALL
SELECT
    c.org_id,
    c.max_duration_default_sec,
    c.candidate_min_power_w,
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
    AND ct.state NOT IN ('resolved', 'restore_failed', 'released')
UNION
SELECT d.device_identifier
FROM curtailment_event ce
JOIN device d ON d.org_id = ce.org_id
    AND d.deleted_at IS NULL
    AND (
        ce.scope_type = 'whole_org'
        OR (
            ce.scope_type = 'site'
            AND d.site_id = (ce.scope_jsonb->>'site_id')::BIGINT
        )
        OR (
            ce.scope_type = 'mixed'
            AND d.site_id IN (
                SELECT mixed_site.site_id::BIGINT
                FROM jsonb_array_elements_text(
                    CASE WHEN jsonb_typeof(ce.scope_jsonb->'site_ids') = 'array'
                      THEN ce.scope_jsonb->'site_ids'
                      ELSE '[]'::jsonb
                    END
                ) AS mixed_site(site_id)
            )
            AND NOT EXISTS (
                SELECT 1
                FROM jsonb_array_elements_text(
                    CASE WHEN jsonb_typeof(ce.scope_jsonb->'device_identifiers') = 'array'
                      THEN ce.scope_jsonb->'device_identifiers'
                      ELSE '[]'::jsonb
                    END
                ) AS mixed_device(device_identifier)
            )
            AND NOT EXISTS (
                SELECT 1
                FROM jsonb_array_elements_text(
                    CASE WHEN jsonb_typeof(ce.scope_jsonb->'device_set_ids') = 'array'
                      THEN ce.scope_jsonb->'device_set_ids'
                      ELSE '[]'::jsonb
                    END
                ) AS mixed_device_set(device_set_id)
            )
        )
    )
WHERE ce.org_id = sqlc.arg('org_id')
    AND ce.state IN ('pending', 'active', 'restoring')
    AND ce.mode = 'FULL_FLEET'
    AND ce.loop_type = 'closed'
    -- All-paired policies keep the scope lock so miners that became
    -- paired-like between admission ticks stay owned before their target row
    -- exists. RELEASED policy rows stay suppressed while the event is
    -- pending/active: release-on-unpair is temporary — the admission pass
    -- reopens the row when the miner is paired-like again, and exempting it
    -- here would let a regular start claim the miner in the gap between
    -- re-pairing and the next (batch-interval-gated) reopen. Only during the
    -- restoring wind-down, when admission no longer runs and reopen is
    -- impossible, does a released row (released without dispatch at Stop)
    -- free the device for other events instead of holding it until terminal.
    AND NOT (
        ce.force_include_all_paired_miners
        AND ce.state = 'restoring'
        AND EXISTS (
            SELECT 1
            FROM curtailment_target released_target
            WHERE released_target.curtailment_event_id = ce.id
              AND released_target.device_identifier = d.device_identifier
              AND released_target.state = 'released'
        )
    );

-- name: ListActiveCurtailmentTargetDevicesByOrg :many
-- Devices with concrete non-terminal target rows; used by closed-loop
-- admission to skip miners already owned by other events without excluding
-- the current targetless scope watcher.
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

-- name: ListRecentlyResolvedCurtailedDevicesByScope :many
-- Scoped cooldown lookup: enumerate the request's live candidate devices first,
-- then probe terminal target history by device identifier.
WITH scoped_devices AS MATERIALIZED (
    SELECT unnest(sqlc.narg('device_identifiers')::text[]) AS device_identifier
    WHERE sqlc.narg('device_identifiers')::text[] IS NOT NULL
    UNION
    SELECT d.device_identifier
    FROM device d
    WHERE d.org_id = sqlc.arg('org_id')
        AND d.deleted_at IS NULL
        AND sqlc.narg('site_ids')::BIGINT[] IS NOT NULL
        AND d.site_id = ANY(sqlc.narg('site_ids')::BIGINT[])
)
SELECT DISTINCT ct.device_identifier
FROM scoped_devices sd
JOIN curtailment_target ct ON ct.device_identifier = sd.device_identifier
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
    curtail_batch_size,
    curtail_batch_interval_sec,
    last_curtail_pending_dispatch_at,
    restore_batch_size,
    restore_batch_interval_sec,
    min_curtailed_duration_sec,
    max_duration_seconds,
    allow_unbounded,
    include_maintenance,
    force_include_maintenance,
    force_include_all_paired_miners,
    facility_fan_device_ids,
    facility_fan_site_ids,
    fan_off_delay_sec,
    fan_restore_delay_sec,
    decision_snapshot_jsonb,
    source_actor_type,
    source_actor_id,
    external_source,
    external_reference,
    idempotency_key,
    reason,
    scheduled_start_at,
    started_at,
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
    sqlc.narg('curtail_batch_size'),
    sqlc.arg('curtail_batch_interval_sec'),
    NULL,
    sqlc.arg('restore_batch_size'),
    sqlc.arg('restore_batch_interval_sec'),
    sqlc.arg('min_curtailed_duration_sec'),
    sqlc.narg('max_duration_seconds'),
    sqlc.arg('allow_unbounded'),
    sqlc.arg('include_maintenance'),
    sqlc.arg('force_include_maintenance'),
    sqlc.arg('force_include_all_paired_miners'),
    sqlc.arg('facility_fan_device_ids'),
    sqlc.arg('facility_fan_site_ids'),
    sqlc.arg('fan_off_delay_sec'),
    sqlc.arg('fan_restore_delay_sec'),
    sqlc.arg('decision_snapshot_jsonb'),
    sqlc.arg('source_actor_type'),
    sqlc.narg('source_actor_id'),
    sqlc.narg('external_source'),
    sqlc.narg('external_reference'),
    sqlc.narg('idempotency_key'),
    sqlc.arg('reason'),
    sqlc.narg('scheduled_start_at'),
    sqlc.narg('started_at'),
    sqlc.narg('ended_at'),
    sqlc.arg('created_by_user_id'),
    sqlc.arg('effective_batch_size')
)
RETURNING id, event_uuid, created_at, updated_at;

-- name: LockCurtailmentFanDeviceForWrite :exec
-- Per-device transaction lock closes concurrent Start races before the array
-- overlap check. Callers acquire these in ascending ID order.
SELECT pg_advisory_xact_lock(hashtextextended('curtailment-fan:' || sqlc.arg('infrastructure_device_id')::TEXT, 0));

-- name: LockCurtailmentFanDevicesForWrite :many
-- The row lock turns the authorization snapshot into an insert-time invariant:
-- a concurrent move/delete must wait until this transaction commits.
SELECT id, site_id
FROM infrastructure_device
WHERE org_id = sqlc.arg('org_id')
  AND id = ANY(sqlc.arg('infrastructure_device_ids')::BIGINT[])
  AND deleted_at IS NULL
ORDER BY id
FOR UPDATE;

-- name: CountConflictingCurtailmentFanClaims :one
SELECT COUNT(*)
FROM curtailment_event
WHERE org_id = sqlc.arg('org_id')
  AND id <> sqlc.arg('excluded_event_id')
  AND (
    state IN ('pending', 'active', 'restoring')
    OR fan_last_error IS NOT NULL
  )
  AND facility_fan_device_ids && sqlc.arg('facility_fan_device_ids')::BIGINT[];

-- name: GetCurtailmentEventByUUID :one
-- Org-scoped: callers MUST pass org_id to prevent cross-tenant exposure.
SELECT *
FROM curtailment_event
WHERE event_uuid = sqlc.arg('event_uuid')
    AND org_id = sqlc.arg('org_id');

-- name: LockCurtailmentEventByUUIDForWrite :one
SELECT *
FROM curtailment_event
WHERE event_uuid = sqlc.arg('event_uuid')
    AND org_id = sqlc.arg('org_id')
FOR UPDATE;

-- name: GetCurtailmentEventDetailByUUID :one
-- Detail reads keep target rows paginated; collapse per-device skipped
-- candidates at the SQL boundary so a single large event cannot force a
-- fleet-sized JSON snapshot through the handler.
SELECT
    id, event_uuid, org_id, state, mode, strategy, level, priority,
    loop_type, scope_type, scope_jsonb, mode_params_jsonb,
    curtail_batch_size, curtail_batch_interval_sec, last_curtail_pending_dispatch_at,
    restore_batch_size, restore_batch_interval_sec, effective_batch_size,
    min_curtailed_duration_sec, max_duration_seconds, allow_unbounded,
    include_maintenance, force_include_maintenance, force_include_all_paired_miners,
    facility_fan_device_ids, facility_fan_site_ids,
    fan_off_delay_sec, fan_restore_delay_sec,
    fan_off_sent_at, fan_on_sent_at, fan_airflow_reopened_at, fan_last_error,
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
    last_error = sqlc.arg('last_error')::TEXT,
    curtail_state = CASE
        WHEN desired_state = 'curtailed' THEN 'restore_failed'
        ELSE curtail_state
    END,
    curtail_completed_at = CASE
        WHEN desired_state = 'curtailed' THEN CURRENT_TIMESTAMP
        ELSE curtail_completed_at
    END,
    curtail_failure_count = CASE
        WHEN desired_state = 'curtailed' THEN curtail_failure_count + 1
        ELSE curtail_failure_count
    END,
    curtail_last_error = CASE
        WHEN desired_state = 'curtailed' THEN sqlc.arg('last_error')::TEXT
        ELSE curtail_last_error
    END,
    restore_state = CASE
        WHEN desired_state = 'active' THEN 'restore_failed'
        ELSE restore_state
    END,
    restore_completed_at = CASE
        WHEN desired_state = 'active' THEN CURRENT_TIMESTAMP
        ELSE restore_completed_at
    END,
    restore_failure_count = CASE
        WHEN desired_state = 'active' THEN restore_failure_count + 1
        ELSE restore_failure_count
    END,
    restore_last_error = CASE
        WHEN desired_state = 'active' THEN sqlc.arg('last_error')::TEXT
        ELSE restore_last_error
    END
WHERE curtailment_event_id = sqlc.arg('curtailment_event_id')
    AND state NOT IN ('resolved', 'restore_failed', 'released');

-- name: ForceReleaseCurtailmentEvent :one
-- Last-resort recovery: persistently releases curtailment ownership for any
-- non-terminal event row. Unlike AdminTerminateCurtailmentEvent, this
-- intentionally supports ACTIVE events and has no in-flight command gate because
-- the operator intent is to clear policy ownership, not report graceful restore.
UPDATE curtailment_event
SET state      = 'cancelled',
    ended_at   = COALESCE(ended_at, NOW()),
    updated_at = NOW()
WHERE event_uuid = sqlc.arg('event_uuid')
  AND org_id = sqlc.arg('org_id')
  AND state IN ('pending', 'active', 'restoring')
RETURNING *;

-- name: SweepCurtailmentTargetsToReleased :execrows
-- Force every non-terminal target → RELEASED with the operator reason. This
-- releases ownership without claiming that restore was attempted or failed.
UPDATE curtailment_target
SET state      = 'released',
    last_error = sqlc.arg('last_error')::TEXT,
    curtail_state = CASE
        WHEN desired_state = 'curtailed' THEN 'released'
        ELSE curtail_state
    END,
    curtail_completed_at = CASE
        WHEN desired_state = 'curtailed' THEN COALESCE(curtail_completed_at, CURRENT_TIMESTAMP)
        ELSE curtail_completed_at
    END,
    restore_state = CASE
        WHEN desired_state = 'active' THEN 'released'
        ELSE restore_state
    END,
    restore_completed_at = CASE
        WHEN desired_state = 'active' THEN COALESCE(restore_completed_at, CURRENT_TIMESTAMP)
        ELSE restore_completed_at
    END
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
    curtail_batch_size, curtail_batch_interval_sec, last_curtail_pending_dispatch_at,
    restore_batch_size, restore_batch_interval_sec, effective_batch_size,
    min_curtailed_duration_sec, max_duration_seconds, allow_unbounded,
    include_maintenance, force_include_maintenance, force_include_all_paired_miners,
    facility_fan_device_ids, facility_fan_site_ids,
    fan_off_delay_sec, fan_restore_delay_sec,
    fan_off_sent_at, fan_on_sent_at, fan_airflow_reopened_at, fan_last_error,
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

-- name: ListActiveCurtailmentEvents :many
-- Org-scoped list of every non-terminal event. Multiple can be active when
-- they target disjoint device scopes (e.g. per-site curtailment). Most-recent
-- first by effective time (started_at, or created_at for pending), id tiebreak.
--
-- Active summaries intentionally omit the persisted decision snapshot. Polling
-- runs frequently and the response shape never exposes the snapshot; detail
-- callers use GetCurtailmentEventDetailByUUID instead.
--
-- Each row carries a live per-state target rollup so active polling reflects
-- the event's current target set (closed-loop claims and all-paired policy
-- changes grow it past the event-start snapshot) without hydrating per-target
-- rows. Events with no target rows aggregate to a zeroed rollup.
SELECT
    ce.id, ce.event_uuid, ce.org_id, ce.state, ce.mode, ce.strategy, ce.level, ce.priority,
    ce.loop_type, ce.scope_type, ce.scope_jsonb, ce.mode_params_jsonb,
    ce.curtail_batch_size, ce.curtail_batch_interval_sec, ce.last_curtail_pending_dispatch_at,
    ce.restore_batch_size, ce.restore_batch_interval_sec, ce.effective_batch_size,
    ce.min_curtailed_duration_sec, ce.max_duration_seconds, ce.allow_unbounded,
    ce.include_maintenance, ce.force_include_maintenance, ce.force_include_all_paired_miners,
    ce.facility_fan_device_ids, ce.facility_fan_site_ids,
    ce.fan_off_delay_sec, ce.fan_restore_delay_sec,
    ce.fan_off_sent_at, ce.fan_on_sent_at, ce.fan_airflow_reopened_at, ce.fan_last_error,
    '{}'::JSONB AS decision_snapshot_jsonb,
    ce.source_actor_type, ce.source_actor_id,
    ce.external_source, ce.external_reference, ce.idempotency_key,
    ce.supersedes_event_id, ce.reason, ce.scheduled_start_at, ce.started_at, ce.ended_at,
    ce.created_at, ce.updated_at, ce.created_by_user_id,
    COALESCE(rollup.pending, 0)::BIGINT AS rollup_pending,
    COALESCE(rollup.dispatched, 0)::BIGINT AS rollup_dispatched,
    COALESCE(rollup.confirmed, 0)::BIGINT AS rollup_confirmed,
    COALESCE(rollup.drifted, 0)::BIGINT AS rollup_drifted,
    COALESCE(rollup.resolved, 0)::BIGINT AS rollup_resolved,
    COALESCE(rollup.released, 0)::BIGINT AS rollup_released,
    COALESCE(rollup.restore_failed, 0)::BIGINT AS rollup_restore_failed,
    COALESCE(rollup.unavailable, 0)::BIGINT AS rollup_unavailable,
    COALESCE(rollup.total, 0)::BIGINT AS rollup_total,
    COALESCE(unavailable_reason_rollup.reasons, '{}'::JSONB) AS rollup_unavailable_reasons
FROM curtailment_event ce
LEFT JOIN LATERAL (
    -- State buckets mirror GetCurtailmentTargetRollupByEvent below; keep the
    -- two aggregates' bucketing rules in sync (dispatching/dispatched conflate).
    SELECT
        COUNT(*) FILTER (WHERE ct.state = 'pending') AS pending,
        COUNT(*) FILTER (WHERE ct.state IN ('dispatching', 'dispatched')) AS dispatched,
        COUNT(*) FILTER (WHERE ct.state = 'confirmed') AS confirmed,
        COUNT(*) FILTER (WHERE ct.state = 'drifted') AS drifted,
        COUNT(*) FILTER (WHERE ct.state = 'resolved') AS resolved,
        COUNT(*) FILTER (WHERE ct.state = 'released') AS released,
        COUNT(*) FILTER (WHERE ct.state = 'restore_failed') AS restore_failed,
        COUNT(*) FILTER (WHERE ct.state = 'unavailable') AS unavailable,
        COUNT(*) AS total
    FROM curtailment_target ct
    WHERE ct.curtailment_event_id = ce.id
) rollup ON true
LEFT JOIN LATERAL (
    -- Reason buckets are split from the state rollup so the state counts stay
    -- index-only on idx_curtailment_target_event_state; this aggregate is
    -- covered by idx_curtailment_target_unavailable_reason.
    SELECT jsonb_object_agg(reason, reason_count) AS reasons
    FROM (
        SELECT
            COALESCE(NULLIF(ct.last_error, ''), 'unknown') AS reason,
            COUNT(*)::BIGINT AS reason_count
        FROM curtailment_target ct
        WHERE ct.curtailment_event_id = ce.id
            AND ct.state = 'unavailable'
        GROUP BY reason
    ) reason_counts
) unavailable_reason_rollup ON true
WHERE ce.org_id = sqlc.arg('org_id')
    AND ce.state IN ('pending', 'active', 'restoring')
ORDER BY COALESCE(ce.started_at, ce.created_at) DESC, ce.id DESC;

-- name: ListCurtailmentTargetSiteCoverageByEvent :many
-- Coverage for explicit-device event authorization. target_count is every
-- persisted target row; mapped_target_count includes only targets that still
-- resolve to a live device with a site. Any mismatch fails closed in handlers.
WITH target_sites AS (
    SELECT d.site_id::BIGINT AS site_id
    FROM curtailment_event ce
    JOIN curtailment_target ct ON ct.curtailment_event_id = ce.id
    LEFT JOIN device d ON d.org_id = ce.org_id
        AND d.device_identifier = ct.device_identifier
        AND d.deleted_at IS NULL
    WHERE ce.org_id = sqlc.arg('org_id')
        AND ce.event_uuid = sqlc.arg('event_uuid')
),
coverage AS (
    SELECT
        COUNT(*)::BIGINT AS target_count,
        COUNT(site_id)::BIGINT AS mapped_target_count
    FROM target_sites
),
site_rows AS (
    SELECT DISTINCT site_id
    FROM target_sites
)
SELECT
    COALESCE(site_rows.site_id, 0)::BIGINT AS site_id,
    coverage.target_count,
    coverage.mapped_target_count
FROM site_rows
CROSS JOIN coverage
ORDER BY site_rows.site_id NULLS FIRST;

-- name: ListCurtailmentTargetSiteCoverageByEvents :many
-- Batched coverage for list permission filtering. Events with no persisted
-- target rows are omitted so callers can default them to complete coverage.
WITH target_sites AS (
    SELECT
        ce.event_uuid,
        d.site_id::BIGINT AS site_id
    FROM curtailment_event ce
    JOIN curtailment_target ct ON ct.curtailment_event_id = ce.id
    LEFT JOIN device d ON d.org_id = ce.org_id
        AND d.device_identifier = ct.device_identifier
        AND d.deleted_at IS NULL
    WHERE ce.org_id = sqlc.arg('org_id')
        AND ce.event_uuid = ANY(sqlc.arg('event_uuids')::UUID[])
),
coverage AS (
    SELECT
        event_uuid,
        COUNT(*)::BIGINT AS target_count,
        COUNT(site_id)::BIGINT AS mapped_target_count
    FROM target_sites
    GROUP BY event_uuid
),
site_rows AS (
    SELECT DISTINCT event_uuid, site_id
    FROM target_sites
)
SELECT
    site_rows.event_uuid,
    COALESCE(site_rows.site_id, 0)::BIGINT AS site_id,
    coverage.target_count,
    coverage.mapped_target_count
FROM site_rows
JOIN coverage USING (event_uuid)
ORDER BY site_rows.event_uuid, site_rows.site_id NULLS FIRST;

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
    last_error,
    curtail_state,
    curtail_last_error,
    baseline_power_w,
    selector_rationale_jsonb
)
SELECT
    sqlc.arg('curtailment_event_id'),
    t.device_identifier,
    t.target_type,
    t.state,
    t.desired_state,
    t.last_error,
    t.state,
    t.last_error,
    t.baseline_power_w,
    t.selector_rationale_jsonb
FROM jsonb_to_recordset(sqlc.arg('targets_jsonb')::JSONB) AS t(
    device_identifier         TEXT,
    target_type               TEXT,
    state                     TEXT,
    desired_state             TEXT,
    last_error                TEXT,
    baseline_power_w          NUMERIC(12,3),
    selector_rationale_jsonb  JSONB
)
WHERE sqlc.arg('cooldown_sec')::INT <= 0
    OR NOT EXISTS (
        SELECT 1
        FROM curtailment_target cooldown_target
        JOIN curtailment_event cooldown_event
            ON cooldown_event.id = cooldown_target.curtailment_event_id
        WHERE cooldown_event.org_id = sqlc.arg('org_id')
            AND cooldown_target.device_identifier = t.device_identifier
            AND cooldown_target.state IN ('resolved', 'restore_failed')
            AND (
                cooldown_event.state IN ('pending', 'active', 'restoring')
                OR cooldown_event.ended_at >= CURRENT_TIMESTAMP - (sqlc.arg('cooldown_sec')::INT * INTERVAL '1 second')
            )
    );

-- name: LockCurtailmentScopeForWrite :exec
-- Serialize hierarchy start checks by org so conflict detection and event
-- insertion happen under one database-backed critical section.
SELECT pg_advisory_xact_lock(hashtextextended('curtailment_scope:' || sqlc.arg('org_id')::text, 0));

-- name: CountCurtailmentScopeConflicts :one
-- Hierarchy for currently supported closed-loop scopes: org > site.
-- A new whole-org event conflicts with existing whole-org, site, or site-only mixed events.
-- A new site or site-only mixed event conflicts with existing whole-org or overlapping site ownership.
SELECT count(*)::BIGINT
FROM curtailment_event
WHERE org_id = sqlc.arg('org_id')
  AND state IN ('pending', 'active', 'restoring')
  AND mode = 'FULL_FLEET'
  AND loop_type = 'closed'
  AND sqlc.arg('mode')::TEXT = 'FULL_FLEET'
  AND sqlc.arg('loop_type')::TEXT = 'closed'
  AND (
    (
      sqlc.arg('scope_type')::TEXT = 'whole_org'
      AND (
        scope_type IN ('whole_org', 'site')
        OR (
          scope_type = 'mixed'
          AND jsonb_array_length(
            CASE WHEN jsonb_typeof(scope_jsonb->'site_ids') = 'array'
              THEN scope_jsonb->'site_ids'
              ELSE '[]'::jsonb
            END
          ) > 0
          AND jsonb_array_length(
            CASE WHEN jsonb_typeof(scope_jsonb->'device_identifiers') = 'array'
              THEN scope_jsonb->'device_identifiers'
              ELSE '[]'::jsonb
            END
          ) = 0
          AND jsonb_array_length(
            CASE WHEN jsonb_typeof(scope_jsonb->'device_set_ids') = 'array'
              THEN scope_jsonb->'device_set_ids'
              ELSE '[]'::jsonb
            END
          ) = 0
        )
      )
    )
    OR (
      sqlc.arg('scope_type')::TEXT IN ('site', 'mixed')
      AND (
        scope_type = 'whole_org'
        OR (
          scope_type = 'site'
          AND (scope_jsonb->>'site_id')::BIGINT = ANY(sqlc.arg('site_ids')::BIGINT[])
        )
        OR (
          scope_type = 'mixed'
          AND jsonb_array_length(
            CASE WHEN jsonb_typeof(scope_jsonb->'device_identifiers') = 'array'
              THEN scope_jsonb->'device_identifiers'
              ELSE '[]'::jsonb
            END
          ) = 0
          AND jsonb_array_length(
            CASE WHEN jsonb_typeof(scope_jsonb->'device_set_ids') = 'array'
              THEN scope_jsonb->'device_set_ids'
              ELSE '[]'::jsonb
            END
          ) = 0
          AND EXISTS (
            SELECT 1
            FROM jsonb_array_elements_text(
              CASE WHEN jsonb_typeof(scope_jsonb->'site_ids') = 'array'
                THEN scope_jsonb->'site_ids'
                ELSE '[]'::jsonb
              END
            ) AS existing_site_id(site_id)
            WHERE existing_site_id.site_id::BIGINT = ANY(sqlc.arg('site_ids')::BIGINT[])
          )
        )
      )
    )
  );

-- name: ClaimClosedLoopFullFleetTargets :many
-- Closed-loop FULL_FLEET dispatch claim. Locks the parent event so
-- Stop/AdminTerminate and dynamic target claims serialize on lifecycle state.
-- Same-event duplicates and cross-event target conflicts are no-ops; the
-- reconciler retries on a later tick if a conflicting event resolves.
WITH locked_event AS MATERIALIZED (
    SELECT
        curtailment_event.id,
        curtailment_event.org_id,
        curtailment_event.decision_snapshot_jsonb
    FROM curtailment_event
    WHERE curtailment_event.id = sqlc.arg('curtailment_event_id')
      AND curtailment_event.state IN ('pending', 'active')
      AND curtailment_event.mode = 'FULL_FLEET'
      AND curtailment_event.loop_type = 'closed'
    FOR UPDATE
)
INSERT INTO curtailment_target (
    curtailment_event_id,
    device_identifier,
    target_type,
    state,
    desired_state,
    last_error,
    curtail_state,
    curtail_last_error,
    baseline_power_w,
    selector_rationale_jsonb
)
SELECT
    locked_event.id,
    t.device_identifier,
    t.target_type,
    'dispatching',
    t.desired_state,
    t.last_error,
    'dispatching',
    t.last_error,
    t.baseline_power_w,
    t.selector_rationale_jsonb
FROM locked_event
JOIN jsonb_to_recordset(sqlc.arg('targets_jsonb')::JSONB) AS t(
    device_identifier         TEXT,
    target_type               TEXT,
    state                     TEXT,
    desired_state             TEXT,
    last_error                TEXT,
    baseline_power_w          NUMERIC(12,3),
    selector_rationale_jsonb  JSONB
) ON TRUE
WHERE NOT EXISTS (
    SELECT 1
    FROM curtailment_target existing
    WHERE existing.curtailment_event_id = locked_event.id
      AND existing.device_identifier = t.device_identifier
)
ON CONFLICT DO NOTHING
RETURNING curtailment_target.*;

-- name: ClaimAllPairedPolicyTargets :one
-- Durable all-paired FULL_FLEET admission. Inserts targets in their computed
-- policy state (pending or unavailable) instead of immediately claiming them
-- as DISPATCHING. Same-event RELEASED rows may be reopened during a recurtail;
-- other same-event rows and cross-event conflicts are no-ops.
WITH locked_event AS MATERIALIZED (
    SELECT
        curtailment_event.id,
        curtailment_event.org_id
    FROM curtailment_event
    WHERE curtailment_event.id = sqlc.arg('curtailment_event_id')
      AND curtailment_event.state IN ('pending', 'active')
      AND curtailment_event.mode = 'FULL_FLEET'
      AND curtailment_event.loop_type = 'closed'
      AND curtailment_event.force_include_all_paired_miners
    FOR UPDATE
),
reopened AS (
    UPDATE curtailment_target target
    SET state                = t.state,
        desired_state        = t.desired_state,
        last_error           = t.last_error,
        baseline_power_w     = t.baseline_power_w,
        released_at          = NULL,
        retry_count          = 0,
        last_dispatched_at   = NULL,
        last_batch_uuid      = NULL,
        confirmed_at         = NULL,
        curtail_state        = t.state,
        curtail_dispatched_at = NULL,
        curtail_batch_uuid    = NULL,
        curtail_completed_at  = NULL,
        curtail_retry_count   = 0,
        curtail_failure_count = 0,
        curtail_last_error    = t.last_error
    FROM locked_event
    JOIN jsonb_to_recordset(sqlc.arg('targets_jsonb')::JSONB) AS t(
        device_identifier         TEXT,
        target_type               TEXT,
        state                     TEXT,
        desired_state             TEXT,
        last_error                TEXT,
        baseline_power_w          NUMERIC(12,3),
        selector_rationale_jsonb  JSONB
    ) ON TRUE
    WHERE target.curtailment_event_id = locked_event.id
      AND target.device_identifier = t.device_identifier
      AND target.state = 'released'
      AND NOT EXISTS (
          SELECT 1
          FROM curtailment_target other_target
          JOIN curtailment_event other_event
              ON other_event.id = other_target.curtailment_event_id
          WHERE other_target.device_identifier = t.device_identifier
            AND other_target.curtailment_event_id <> locked_event.id
            AND other_event.state IN ('pending', 'active', 'restoring')
            AND other_target.state NOT IN ('resolved', 'restore_failed', 'released')
      )
    RETURNING 1
),
inserted AS (
INSERT INTO curtailment_target (
    curtailment_event_id,
    device_identifier,
    target_type,
    state,
    desired_state,
    last_error,
    curtail_state,
    curtail_last_error,
    baseline_power_w,
    selector_rationale_jsonb
)
SELECT
    locked_event.id,
    t.device_identifier,
    t.target_type,
    t.state,
    t.desired_state,
    t.last_error,
    t.state,
    t.last_error,
    t.baseline_power_w,
    t.selector_rationale_jsonb
FROM locked_event
JOIN jsonb_to_recordset(sqlc.arg('targets_jsonb')::JSONB) AS t(
    device_identifier         TEXT,
    target_type               TEXT,
    state                     TEXT,
    desired_state             TEXT,
    last_error                TEXT,
    baseline_power_w          NUMERIC(12,3),
    selector_rationale_jsonb  JSONB
) ON TRUE
WHERE NOT EXISTS (
    SELECT 1
    FROM curtailment_target existing
    WHERE existing.curtailment_event_id = locked_event.id
      AND existing.device_identifier = t.device_identifier
)
ON CONFLICT DO NOTHING
RETURNING 1
)
SELECT ((SELECT COUNT(*) FROM reopened) + (SELECT COUNT(*) FROM inserted))::BIGINT AS claimed_count;

-- name: BulkRefreshAllPairedTargetReadiness :many
-- Per-tick readiness refresh for all-paired policy rows, batched into one
-- statement so a mass readiness flip (fleet-wide recovery or outage) does not
-- issue one UPDATE round trip per device inside the shared tick budget.
--
-- Guards mirror UpdateCurtailmentTargetState for this transition class:
-- the parent event is locked and must still be in the caller's expected
-- state, and each row must still be a refreshable policy row
-- (desired_state='curtailed', state pending/unavailable). Rows that advanced
-- concurrently (dispatch claim, Stop reset, release) are skipped; the next
-- tick re-reads them. RETURNING reports exactly which rows applied so the
-- reconciler mirrors only those — a skipped row must not be treated as
-- promoted and dispatched against stale state. Empty last_error is the
-- explicit clear sentinel (pending promotion); non-empty increments
-- curtail_failure_count exactly like the per-row query.
--
-- baseline_power_w backfills only NULL baselines: targets inserted while
-- unavailable carry no pre-curtail baseline (power was unknown), so the
-- promotion supplies current telemetry — without it, drift/confirm checks
-- degrade to the hash-only fallback forever. An existing baseline is never
-- overwritten; readiness flaps must not capture asleep-power as baseline.
WITH locked_event AS MATERIALIZED (
    SELECT id
    FROM curtailment_event
    WHERE id = sqlc.arg('curtailment_event_id')
      AND state IN ('pending', 'active')
      AND state = sqlc.arg('expected_event_state')::TEXT
    FOR UPDATE
)
UPDATE curtailment_target AS target
SET state              = t.state,
    last_error         = NULLIF(t.last_error, ''),
    baseline_power_w   = COALESCE(target.baseline_power_w, t.baseline_power_w),
    curtail_state      = t.state,
    curtail_failure_count = CASE
        WHEN NULLIF(t.last_error, '') IS NOT NULL THEN target.curtail_failure_count + 1
        ELSE target.curtail_failure_count
    END,
    curtail_last_error = CASE
        WHEN NULLIF(t.last_error, '') IS NOT NULL THEN t.last_error
        ELSE target.curtail_last_error
    END
FROM locked_event
JOIN jsonb_to_recordset(sqlc.arg('updates_jsonb')::JSONB) AS t(
    device_identifier TEXT,
    state             TEXT,
    last_error        TEXT,
    baseline_power_w  NUMERIC(12,3)
) ON TRUE
WHERE target.curtailment_event_id = locked_event.id
  AND target.device_identifier = t.device_identifier
  AND target.desired_state = 'curtailed'
  AND target.state IN ('pending', 'unavailable')
  AND t.state IN ('pending', 'unavailable')
RETURNING target.device_identifier;

-- name: ListCurtailmentTargetsByEvent :many
-- Org-scoped via the join.
SELECT ct.*
FROM curtailment_target ct
JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
WHERE ce.org_id = sqlc.arg('org_id')
    AND ce.event_uuid = sqlc.arg('event_uuid')
ORDER BY ct.device_identifier;

-- name: ListCurtailmentTargetsByEventPage :many
-- Org-scoped, cursor-paginated target detail for large activity expansion.
SELECT ct.*
FROM curtailment_target ct
JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
WHERE ce.org_id = sqlc.arg('org_id')
    AND ce.event_uuid = sqlc.arg('event_uuid')
    AND (
        sqlc.arg('cursor_device_identifier')::TEXT = ''
        OR ct.device_identifier > sqlc.arg('cursor_device_identifier')::TEXT
    )
ORDER BY ct.device_identifier
LIMIT sqlc.arg('row_limit')::BIGINT;

-- name: GetCurtailmentTargetRollupByEvent :one
-- Org-scoped aggregate for paginated event detail. Target pages can be
-- partial, but the rollup must describe the whole event.
--
-- Both aggregates are lateral subqueries correlated only on ce.id — the same
-- shape as the ListActiveCurtailmentEvents rollup above — so each executes
-- exactly once for the single matched event instead of joining the per-target
-- row stream (which lets the planner rescan a lateral per target row on
-- fleet-sized events; this query sits on the polled event-detail path). The
-- state aggregate touches only index columns of
-- idx_curtailment_target_event_state and stays index-only-scannable; keep the
-- state bucketing rules in sync with the active-list rollup
-- (dispatching/dispatched conflate). Unavailable reason buckets are covered
-- by idx_curtailment_target_unavailable_reason.
SELECT
    COALESCE(state_rollup.pending, 0)::BIGINT AS pending,
    COALESCE(state_rollup.dispatched, 0)::BIGINT AS dispatched,
    COALESCE(state_rollup.confirmed, 0)::BIGINT AS confirmed,
    COALESCE(state_rollup.drifted, 0)::BIGINT AS drifted,
    COALESCE(state_rollup.resolved, 0)::BIGINT AS resolved,
    COALESCE(state_rollup.released, 0)::BIGINT AS released,
    COALESCE(state_rollup.restore_failed, 0)::BIGINT AS restore_failed,
    COALESCE(state_rollup.unavailable, 0)::BIGINT AS unavailable,
    COALESCE(state_rollup.total, 0)::BIGINT AS total,
    COALESCE(unavailable_reason_rollup.reasons, '{}'::JSONB) AS unavailable_reasons
FROM curtailment_event ce
LEFT JOIN LATERAL (
    SELECT
        COUNT(*) FILTER (WHERE ct.state = 'pending') AS pending,
        COUNT(*) FILTER (WHERE ct.state IN ('dispatching', 'dispatched')) AS dispatched,
        COUNT(*) FILTER (WHERE ct.state = 'confirmed') AS confirmed,
        COUNT(*) FILTER (WHERE ct.state = 'drifted') AS drifted,
        COUNT(*) FILTER (WHERE ct.state = 'resolved') AS resolved,
        COUNT(*) FILTER (WHERE ct.state = 'released') AS released,
        COUNT(*) FILTER (WHERE ct.state = 'restore_failed') AS restore_failed,
        COUNT(*) FILTER (WHERE ct.state = 'unavailable') AS unavailable,
        COUNT(*) AS total
    FROM curtailment_target ct
    WHERE ct.curtailment_event_id = ce.id
) state_rollup ON true
LEFT JOIN LATERAL (
    SELECT jsonb_object_agg(reason, reason_count) AS reasons
    FROM (
        SELECT
            COALESCE(NULLIF(ct.last_error, ''), 'unknown') AS reason,
            COUNT(*)::BIGINT AS reason_count
        FROM curtailment_target ct
        WHERE ct.curtailment_event_id = ce.id
            AND ct.state = 'unavailable'
        GROUP BY reason
    ) reason_counts
) unavailable_reason_rollup ON true
WHERE ce.org_id = sqlc.arg('org_id')
    AND ce.event_uuid = sqlc.arg('event_uuid');

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

-- name: RecordCurtailPendingDispatch :execrows
UPDATE curtailment_event
SET last_curtail_pending_dispatch_at = sqlc.arg('dispatched_at')
WHERE id = sqlc.arg('id')
  AND state = sqlc.arg('expected_state')
  AND state IN ('pending', 'active');

-- name: UpdateCurtailmentEventFanState :execrows
-- The expected-state guard prevents a stale reconciler phase from stamping
-- over a concurrent transition. Terminal states remain addressable so an
-- explicit operator Force Release can retry fan ON and clear a durable failure.
-- fan_last_error is always replaced after a successful write.
UPDATE curtailment_event
SET fan_off_sent_at = COALESCE(sqlc.narg('fan_off_sent_at'), fan_off_sent_at),
    fan_on_sent_at = COALESCE(sqlc.narg('fan_on_sent_at'), fan_on_sent_at),
    fan_airflow_reopened_at = CASE
        WHEN sqlc.arg('clear_fan_airflow_reopened_at')::boolean THEN NULL
        ELSE COALESCE(sqlc.narg('fan_airflow_reopened_at'), fan_airflow_reopened_at)
    END,
    fan_last_error = sqlc.narg('fan_last_error'),
    updated_at = NOW()
WHERE id = sqlc.arg('id')
  AND state = sqlc.arg('expected_state');

-- name: LockCurtailmentEventForFanCommand :one
-- Physical fan commands run only while this exact lifecycle phase remains
-- current. Holding the row lock through the command serializes Force Release's
-- terminal UPDATE behind an in-flight command and rejects stale commands that
-- begin after the transition.
SELECT id
FROM curtailment_event
WHERE id = sqlc.arg('id')
  AND state = sqlc.arg('expected_state')
FOR UPDATE;

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
    last_error         = NULL,
    restore_state      = 'pending',
    restore_started_at = CURRENT_TIMESTAMP,
    restore_dispatched_at = NULL,
    restore_batch_uuid    = NULL,
    restore_completed_at  = NULL,
    restore_retry_count   = 0,
    restore_failure_count = 0,
    restore_last_error    = NULL
WHERE curtailment_event_id = sqlc.arg('curtailment_event_id')
  AND state NOT IN ('resolved', 'restore_failed', 'released');

-- name: ReleaseUndispatchedAllPairedTargetsForRestore :execrows
-- All-paired policy targets that never received a Curtail command do not need
-- Uncurtail. Release them before the restore reset so graceful Stop does not
-- enqueue no-op restore work for offline/auth-needed miners.
--
-- "Never attempted" is retry_count = 0 plus NULL dispatch timestamps: every
-- dispatch attempt/failure bumps retry_count and every successful enqueue
-- stamps last_dispatched_at. curtail_failure_count is deliberately NOT
-- checked — readiness flaps (pending -> unavailable reason writes) inflate it
-- without any command ever being sent.
--
-- restore_started_at IS NULL guards the Stop -> Recurtail -> Stop cascade:
-- ResetCurtailmentTargetsForRecurtail wipes retry_count and both dispatch
-- timestamps, making a previously dispatched-and-confirmed (physically
-- powered-off) target indistinguishable from never-attempted. The restore
-- stamp survives that reset — any row that ever entered a restore cycle had
-- a real dispatch in its past and must route through the restore queue, not
-- be terminally released.
UPDATE curtailment_target
SET state              = 'released',
    last_error         = COALESCE(last_error, 'released without restore: no curtail command dispatched'),
    curtail_state      = 'released',
    curtail_completed_at = COALESCE(curtail_completed_at, CURRENT_TIMESTAMP),
    curtail_last_error = COALESCE(curtail_last_error, last_error, 'released without restore: no curtail command dispatched')
WHERE curtailment_event_id = sqlc.arg('curtailment_event_id')
  AND desired_state = 'curtailed'
  AND state IN ('pending', 'unavailable')
  AND last_dispatched_at IS NULL
  AND curtail_dispatched_at IS NULL
  AND retry_count = 0
  AND restore_started_at IS NULL;

-- name: ResumeCurtailmentFromRestoring :one
-- Restore reversal: go back through pending so the curtail dispatcher picks
-- up reset targets. Preserve fan_off_sent_at and fan_last_error until the
-- active reconciler has positively reopened airflow; clearing them here can
-- hide fans that remained off after a failed restore command.
UPDATE curtailment_event
SET state = 'pending',
    fan_airflow_reopened_at = COALESCE(fan_on_sent_at, fan_airflow_reopened_at),
    fan_on_sent_at = NULL,
    last_curtail_pending_dispatch_at = NULL
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
        last_error         = NULL,
        curtail_state      = 'pending',
        curtail_dispatched_at = NULL,
        curtail_batch_uuid    = NULL,
        curtail_completed_at  = NULL,
        curtail_retry_count   = 0,
        curtail_failure_count = 0,
        curtail_last_error    = NULL
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
--
-- expected_state and expected_dispatch_batch_uuid are the confirmation
-- fast-path single-winner guards. When set, the target's current state must
-- equal expected_state and the applicable phase batch UUID (curtail_batch_uuid
-- when desired_state='curtailed', restore_batch_uuid when 'active', consistent
-- with the mirror logic) must equal expected_dispatch_batch_uuid. A duplicate
-- confirmation (state advanced past 'dispatched') or a timeout/redispatch that
-- stamped a new batch UUID (ABA) matches zero rows -> ErrCurtailmentEventState
-- RaceLoss. Both narg guards are inert when NULL, so full-tick writes that omit
-- them are unaffected.
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
    END,
    curtail_state = CASE
        WHEN desired_state = 'curtailed' THEN sqlc.arg('state')
        ELSE curtail_state
    END,
    curtail_dispatched_at = CASE
        WHEN desired_state = 'curtailed' THEN COALESCE(sqlc.narg('last_dispatched_at'), curtail_dispatched_at)
        ELSE curtail_dispatched_at
    END,
    curtail_batch_uuid = CASE
        WHEN desired_state = 'curtailed' THEN COALESCE(sqlc.narg('last_batch_uuid'), curtail_batch_uuid)
        ELSE curtail_batch_uuid
    END,
    curtail_completed_at = CASE
        WHEN desired_state = 'curtailed'
             AND sqlc.arg('state') IN ('confirmed', 'released', 'restore_failed') THEN
            COALESCE(sqlc.narg('confirmed_at'), CURRENT_TIMESTAMP)
        ELSE curtail_completed_at
    END,
    curtail_retry_count = CASE
        WHEN desired_state = 'curtailed' THEN COALESCE(sqlc.narg('retry_count'), curtail_retry_count)
        ELSE curtail_retry_count
    END,
    curtail_failure_count = CASE
        WHEN desired_state = 'curtailed'
             AND NULLIF(sqlc.narg('last_error')::text, '') IS NOT NULL THEN curtail_failure_count + 1
        ELSE curtail_failure_count
    END,
    curtail_last_error = CASE
        WHEN desired_state = 'curtailed'
             AND NULLIF(sqlc.narg('last_error')::text, '') IS NOT NULL THEN NULLIF(sqlc.narg('last_error')::text, '')
        ELSE curtail_last_error
    END,
    restore_state = CASE
        WHEN desired_state = 'active' THEN sqlc.arg('state')
        ELSE restore_state
    END,
    restore_dispatched_at = CASE
        WHEN desired_state = 'active' THEN COALESCE(sqlc.narg('last_dispatched_at'), restore_dispatched_at)
        ELSE restore_dispatched_at
    END,
    restore_batch_uuid = CASE
        WHEN desired_state = 'active' THEN COALESCE(sqlc.narg('last_batch_uuid'), restore_batch_uuid)
        ELSE restore_batch_uuid
    END,
    restore_completed_at = CASE
        WHEN desired_state = 'active'
             AND sqlc.arg('state') IN ('resolved', 'released', 'restore_failed') THEN
            COALESCE(sqlc.narg('confirmed_at'), CURRENT_TIMESTAMP)
        ELSE restore_completed_at
    END,
    restore_retry_count = CASE
        WHEN desired_state = 'active' THEN COALESCE(sqlc.narg('retry_count'), restore_retry_count)
        ELSE restore_retry_count
    END,
    restore_failure_count = CASE
        WHEN desired_state = 'active'
             AND NULLIF(sqlc.narg('last_error')::text, '') IS NOT NULL THEN restore_failure_count + 1
        ELSE restore_failure_count
    END,
    restore_last_error = CASE
        WHEN desired_state = 'active'
             AND NULLIF(sqlc.narg('last_error')::text, '') IS NOT NULL THEN NULLIF(sqlc.narg('last_error')::text, '')
        ELSE restore_last_error
    END
FROM locked_event
WHERE curtailment_event_id = locked_event.id
  AND device_identifier    = sqlc.arg('device_identifier')
  AND (sqlc.narg('expected_desired_state')::text IS NULL
       OR desired_state = sqlc.narg('expected_desired_state')::text)
  AND (sqlc.narg('expected_state')::text IS NULL
       OR state = sqlc.narg('expected_state')::text)
  AND (sqlc.narg('expected_dispatch_batch_uuid')::text IS NULL
       OR (CASE
               WHEN desired_state = 'curtailed' THEN curtail_batch_uuid
               WHEN desired_state = 'active'    THEN restore_batch_uuid
           END) = sqlc.narg('expected_dispatch_batch_uuid')::text);

-- name: BumpCurtailmentTargetRetry :execrows
-- Fallback when UpdateCurtailmentTargetState fails non-race-loss:
-- advance retry_count alone so MaxRetries → RESTORE_FAILED escalation
-- still lands on the next successful state-change write. EXISTS guard
-- → zero rows → ErrCurtailmentEventStateRaceLoss on terminal parent.
UPDATE curtailment_target
SET retry_count = retry_count + 1,
    curtail_retry_count = CASE
        WHEN desired_state = 'curtailed' THEN curtail_retry_count + 1
        ELSE curtail_retry_count
    END,
    restore_retry_count = CASE
        WHEN desired_state = 'active' THEN restore_retry_count + 1
        ELSE restore_retry_count
    END
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
-- (15-min window). site_ids and device_identifiers nil = whole-org.
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
        (sqlc.narg('site_ids')::BIGINT[] IS NULL AND sqlc.narg('device_identifiers')::text[] IS NULL)
        OR (
            sqlc.narg('site_ids')::BIGINT[] IS NOT NULL
            AND d.site_id = ANY(sqlc.narg('site_ids')::BIGINT[])
        )
        OR (
            sqlc.narg('device_identifiers')::text[] IS NOT NULL
            AND d.device_identifier = ANY(sqlc.narg('device_identifiers')::text[])
        )
    )
-- Stable order so the selector's stable sort is deterministic on ties.
ORDER BY d.device_identifier;

-- name: ListEligibleConfirmationTargets :many
-- System-scope (no org filter); reconciler-only fast-path confirmation read.
-- MUST NOT be exposed through any RPC handler.
--
-- Returns only phase-valid `dispatched` targets the confirmation pulse may
-- promote:
--   * curtail work: pending/active event + target desired_state='curtailed'
--     with a durable curtail_dispatched_at + curtail_batch_uuid;
--   * restore work: restoring event + target desired_state='active' with a
--     durable restore_dispatched_at + restore_batch_uuid.
-- phase_dispatched_at / phase_batch_uuid select the columns for the row's
-- phase so the pulse bounds sample freshness and guards the promoting write
-- on the exact applicable batch UUID. baseline_power_w and pairing_status
-- feed the same confirmation predicates the full tick uses; pairing_status
-- joins like ListCurtailmentCandidatesByOrg (missing device -> 'UNPAIRED').
SELECT
    ce.id                              AS event_id,
    ce.event_uuid                      AS event_uuid,
    ce.org_id                          AS org_id,
    ce.state                           AS event_state,
    ce.force_include_all_paired_miners AS force_include_all_paired_miners,
    ct.device_identifier               AS device_identifier,
    ct.desired_state                   AS desired_state,
    ct.baseline_power_w                AS baseline_power_w,
    (CASE
        WHEN ce.state IN ('pending', 'active') THEN ct.curtail_dispatched_at
        ELSE ct.restore_dispatched_at
     END)::timestamptz                 AS phase_dispatched_at,
    (CASE
        WHEN ce.state IN ('pending', 'active') THEN ct.curtail_batch_uuid
        ELSE ct.restore_batch_uuid
     END)::text                        AS phase_batch_uuid,
    CASE WHEN dp.id IS NOT NULL THEN dp.pairing_status::text ELSE 'UNPAIRED' END AS pairing_status
FROM curtailment_target ct
INNER JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
LEFT JOIN device d ON d.device_identifier = ct.device_identifier
    AND d.deleted_at IS NULL
LEFT JOIN device_pairing dp ON dp.device_id = d.id
WHERE ct.state = 'dispatched'
  AND (
        (ce.state IN ('pending', 'active')
            AND ct.desired_state = 'curtailed'
            AND ct.curtail_dispatched_at IS NOT NULL
            AND ct.curtail_batch_uuid IS NOT NULL)
     OR (ce.state = 'restoring'
            AND ct.desired_state = 'active'
            AND ct.restore_dispatched_at IS NOT NULL
            AND ct.restore_batch_uuid IS NOT NULL)
      )
ORDER BY ce.id, ct.device_identifier;
