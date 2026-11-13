-- name: RecordTelemetryFreshnessBatch :exec
-- Sort by device id to give overlapping batches the same lock order. Unknown
-- observations cannot reset freshness; old/replayed observations cannot advance
-- the watermark. Both observation and ingestion times are coalesced for 30s,
-- except on route changes or the first known observation. A stored watermark
-- can therefore lag by approximately 30s plus polling cadence and source age.
INSERT INTO telemetry_freshness(device_id,observed_at,ingested_at,observation_known,source_node_id)
SELECT d.id, max(CASE WHEN samples.known THEN samples.observed_at END), clock_timestamp(), bool_or(samples.known), max(samples.source_node_id)
FROM (SELECT unnest(sqlc.arg('identifiers')::text[]) AS identifier,
             unnest(sqlc.arg('observed_times')::timestamptz[]) AS observed_at,
             unnest(sqlc.arg('known')::boolean[]) AS known,
             unnest(sqlc.arg('source_nodes')::bigint[]) AS source_node_id) samples
JOIN device d ON d.device_identifier=samples.identifier AND d.deleted_at IS NULL
LEFT JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
WHERE samples.source_node_id=COALESCE(fnd.fleet_node_id,0)
GROUP BY d.id ORDER BY d.id
ON CONFLICT (device_id) DO UPDATE SET
 observed_at=CASE WHEN telemetry_freshness.source_node_id=EXCLUDED.source_node_id
                  THEN GREATEST(telemetry_freshness.observed_at,EXCLUDED.observed_at) ELSE EXCLUDED.observed_at END,
 source_node_id=EXCLUDED.source_node_id,
 ingested_at=EXCLUDED.ingested_at,
 observation_known=CASE WHEN telemetry_freshness.source_node_id=EXCLUDED.source_node_id
                        THEN telemetry_freshness.observation_known OR EXCLUDED.observation_known
                        ELSE EXCLUDED.observation_known END
WHERE telemetry_freshness.source_node_id IS DISTINCT FROM EXCLUDED.source_node_id
 OR (NOT telemetry_freshness.observation_known AND EXCLUDED.observation_known)
 OR EXCLUDED.ingested_at >= telemetry_freshness.ingested_at + INTERVAL '30 seconds';

-- name: GetTelemetryFreshnessSummary :many
SELECT org_id,site_id,source_node_id,device_class,expected_count,fresh_count,stale_count,unknown_count,excluded_count,grace_count,oldest_age_seconds
FROM telemetry_freshness_summary ORDER BY org_id,site_id,source_node_id,device_class;

-- name: CreateTelemetryFreshnessSavepoint :exec
SAVEPOINT telemetry_freshness;

-- name: RollbackTelemetryFreshnessSavepoint :exec
ROLLBACK TO SAVEPOINT telemetry_freshness;

-- name: ReleaseTelemetryFreshnessSavepoint :exec
RELEASE SAVEPOINT telemetry_freshness;
