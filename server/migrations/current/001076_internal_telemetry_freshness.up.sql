-- requires-shared: 1000

CREATE TABLE telemetry_freshness (
    device_id bigint CONSTRAINT pk_telemetry_freshness PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
    observed_at timestamptz,
    source_node_id bigint NOT NULL DEFAULT 0 CHECK (source_node_id >= 0),
    ingested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    observation_known boolean NOT NULL,
    CONSTRAINT ck_telemetry_freshness_observation CHECK (NOT observation_known OR observed_at IS NOT NULL)
);
CREATE TABLE telemetry_freshness_exclusion (
    device_id bigint CONSTRAINT pk_telemetry_freshness_exclusion PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
    reason text NOT NULL CHECK (length(trim(reason)) BETWEEN 1 AND 256),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    CONSTRAINT ck_telemetry_freshness_exclusion_expiry CHECK (expires_at > created_at)
);
-- One compact row per inventory target, never a scan of raw measurement history.
-- Paused hashing and unreachable nodes remain expected. Maintenance is explicit
-- and expires; pairing/assignment starts a five-minute admission grace period.
CREATE VIEW telemetry_freshness_summary AS
WITH inventory AS (
 SELECT d.org_id, COALESCE(d.site_id,0)::bigint AS site_id,
        COALESCE(fnd.fleet_node_id,0)::bigint AS source_node_id, dd.device_class,
        GREATEST(COALESCE(dp.paired_at,dp.created_at,d.created_at,'epoch'::timestamptz),COALESCE(fnd.assigned_at,'epoch'::timestamptz)) AS expected_since,
        tf.observed_at, tf.observation_known, tf.source_node_id AS observed_source_node_id,
        (ex.expires_at > CURRENT_TIMESTAMP) IS TRUE AS excluded
 FROM device d
 JOIN device_pairing dp ON dp.device_id=d.id
 JOIN discovered_device dd ON dd.id=d.discovered_device_id
 LEFT JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
 LEFT JOIN telemetry_freshness tf ON tf.device_id=d.id
 LEFT JOIN telemetry_freshness_exclusion ex ON ex.device_id=d.id
 WHERE d.deleted_at IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD','AUTHENTICATION_NEEDED')
), classified AS (
 SELECT *, expected_since <= CURRENT_TIMESTAMP - INTERVAL '5 minutes' AND NOT excluded AS expected,
        observation_known IS TRUE AND observed_source_node_id=source_node_id AND observed_at >= expected_since AND observed_at <= CURRENT_TIMESTAMP AS known
 FROM inventory
)
SELECT org_id,site_id,source_node_id,device_class,
 count(*) FILTER (WHERE expected)::bigint AS expected_count,
 count(*) FILTER (WHERE expected AND known AND observed_at >= CURRENT_TIMESTAMP - INTERVAL '2 minutes')::bigint AS fresh_count,
 count(*) FILTER (WHERE expected AND known AND observed_at < CURRENT_TIMESTAMP - INTERVAL '2 minutes')::bigint AS stale_count,
 count(*) FILTER (WHERE expected AND NOT known)::bigint AS unknown_count,
 count(*) FILTER (WHERE excluded)::bigint AS excluded_count,
 count(*) FILTER (WHERE NOT excluded AND NOT expected)::bigint AS grace_count,
 COALESCE(max(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP-observed_at)) FILTER (WHERE expected AND known),0)::double precision AS oldest_age_seconds
FROM classified GROUP BY org_id,site_id,source_node_id,device_class;
