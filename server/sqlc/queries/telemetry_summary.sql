-- name: LockTelemetrySeedMaintenance :exec
SELECT pg_advisory_lock(1027,'device_metrics'::regclass::oid::integer);

-- name: UnlockTelemetrySeedMaintenance :exec
SELECT pg_advisory_unlock(1027,'device_metrics'::regclass::oid::integer);

-- name: PrepareTelemetrySeed :one
SELECT prepare_telemetry_seed(sqlc.arg('requested_start')::timestamptz)::timestamptz AS safe_start;

-- name: HasDueTelemetrySeedRecovery :one
SELECT EXISTS(SELECT 1 FROM telemetry_summary_work w JOIN telemetry_summary_progress p USING (aggregate_name)
 WHERE p.source_name='device_metrics' AND w.next_run_at<=clock_timestamp() AND NOT w.blocked)::boolean;

-- name: HasBlockedTelemetrySeedRecovery :one
SELECT EXISTS(SELECT 1 FROM telemetry_summary_work w JOIN telemetry_summary_progress p USING (aggregate_name)
 WHERE p.source_name='device_metrics' AND w.blocked)::boolean;

-- name: RecoverTelemetrySeedBatch :exec
CALL telemetry_summary_step('device_metrics');
