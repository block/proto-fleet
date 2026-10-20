-- name: GetHostedAccountHistoryEpoch :one
SELECT started_at FROM hosted_account_history_epoch WHERE singleton;

-- name: ListHostedAccountHistoryIntervals :many
SELECT i.kind,i.asset_id,i.device_id,i.client_id,COALESCE(i.subclient_id,0)::bigint AS subclient_id,
 i.started_at,i.ended_at,i.device_identifier,COALESCE(i.site_id,0)::bigint AS site_id
FROM hosted_account_asset_interval i
WHERE i.organization_id=sqlc.arg(organization_id)
 AND (i.client_id=sqlc.arg(account_id)::bigint OR i.subclient_id=sqlc.arg(account_id)::bigint)
 AND i.started_at < sqlc.arg(end_time)::timestamptz
 AND (i.ended_at IS NULL OR i.ended_at>sqlc.arg(start_time)::timestamptz)
 AND (NOT sqlc.arg(has_owner_filter)::boolean OR COALESCE(i.subclient_id,0)=sqlc.arg(owner_id)::bigint)
ORDER BY i.kind,i.asset_id,i.started_at;

-- name: ListHostedAccountHistoricalReadings :many
-- The bounded interval list is already authorized to the requested account.
-- Raw samples are filtered by half-open ownership boundaries before aggregation.
WITH intervals AS MATERIALIZED (
 SELECT (value->>'kind')::integer AS kind,(value->>'asset_id')::bigint AS asset_id,(value->>'site_id')::bigint AS site_id,
 (value->>'device_identifier')::text AS device_identifier,
 (value->>'started_at')::timestamptz AS started_at,(value->>'ended_at')::timestamptz AS ended_at
 FROM jsonb_array_elements(sqlc.arg(intervals)::jsonb)
), samples AS (
 SELECT i.kind,i.asset_id,m.time,COALESCE(m.site_id,0)::bigint AS site_id,
 m.hash_rate_hs AS hashrate_hs,m.power_w,m.temp_c AS temperature_c,
 COALESCE(m.hash_rate_hs>=0 AND m.hash_rate_hs<'Infinity'::float8 AND m.power_w>=0 AND m.power_w<'Infinity'::float8,false) AS complete, true AS efficiency_candidate
 FROM intervals i
 JOIN device_metrics m ON i.kind=5 AND m.device_identifier=i.device_identifier AND COALESCE(m.site_id,0)=i.site_id
  AND m.time>=i.started_at AND (i.ended_at IS NULL OR m.time<i.ended_at)
  AND m.time>=sqlc.arg(start_time)::timestamptz AND m.time<sqlc.arg(end_time)::timestamptz
 UNION ALL
 SELECT i.kind,i.asset_id,CASE WHEN source_window.has_source THEN metric.newest_at ELSE m.collected_at END AS time,COALESCE(m.site_id,0)::bigint,
 CASE WHEN metric.name='hashrate' AND source_window.has_source THEN metric.value END,
 CASE WHEN metric.name='power' AND source_window.has_source THEN metric.value END,
 CASE WHEN metric.name='temperature' AND source_window.has_source THEN metric.value END,
 CASE WHEN metric.name='temperature' THEN true ELSE source_window.has_source AND COALESCE(
  (m.coverage->metric.name->>'topology_complete')::boolean
  AND (m.coverage->metric.name->>'expected')::int>0
  AND (m.coverage->metric.name->>'reporting')::int=(m.coverage->metric.name->>'expected')::int
  AND metric.value>=0 AND metric.value<'Infinity'::float8,false) END,
 metric.name IN ('hashrate','power') AS efficiency_candidate
 FROM intervals i
 JOIN container_module_metrics m ON i.kind=6 AND m.module_id=i.asset_id AND COALESCE(m.site_id,0)=i.site_id
  -- The stored time may come from a stale PSU while board sources are fresh.
  -- Bound collection using the source freshness/skew allowances, then authorize
  -- and bucket each metric using its full independent contributor window.
  AND m.collected_at>=sqlc.arg(start_time)::timestamptz-INTERVAL '5 seconds'
  AND m.collected_at<sqlc.arg(end_time)::timestamptz+INTERVAL '2 minutes'
  AND m.collected_at>=i.started_at-INTERVAL '5 seconds'
  AND (i.ended_at IS NULL OR m.collected_at<i.ended_at+INTERVAL '2 minutes')
 CROSS JOIN LATERAL (VALUES
  ('hashrate',m.hashrate_hs,(m.coverage#>>'{hashrate,oldest_at}')::timestamptz,(m.coverage#>>'{hashrate,newest_at}')::timestamptz),
  ('power',m.power_w,(m.coverage#>>'{power,oldest_at}')::timestamptz,(m.coverage#>>'{power,newest_at}')::timestamptz),
  ('temperature',m.temperature_c,(m.coverage#>>'{temperature,oldest_at}')::timestamptz,(m.coverage#>>'{temperature,newest_at}')::timestamptz)
 ) metric(name,value,oldest_at,newest_at)
 CROSS JOIN LATERAL (
  SELECT COALESCE(metric.oldest_at>'0001-01-01T00:00:00Z'::timestamptz
   AND metric.newest_at>=metric.oldest_at,false) AS has_source
 ) source_window
 WHERE (source_window.has_source
  AND metric.oldest_at>=i.started_at AND (i.ended_at IS NULL OR metric.newest_at<i.ended_at)
  AND metric.newest_at>=sqlc.arg(start_time)::timestamptz AND metric.newest_at<sqlc.arg(end_time)::timestamptz)
 OR (
  -- Missing sources carry only an incompleteness marker, never a value.
  -- Authorize that absence at collection time without admitting earlier owners.
  NOT source_window.has_source AND metric.name IN ('hashrate','power')
  AND m.collected_at>=i.started_at AND (i.ended_at IS NULL OR m.collected_at<i.ended_at)
  AND m.collected_at>=sqlc.arg(start_time)::timestamptz AND m.collected_at<sqlc.arg(end_time)::timestamptz
 )

), asset_buckets AS (
SELECT kind,asset_id,date_bin(make_interval(secs=>sqlc.arg(bucket_seconds)::int),time,sqlc.arg(start_time)::timestamptz)::timestamptz AS bucket,
 COALESCE(avg(hashrate_hs) FILTER(WHERE hashrate_hs>=0 AND hashrate_hs<'Infinity'::float8),0)::float8 AS hashrate_hs,
 COALESCE(avg(power_w) FILTER(WHERE power_w>=0 AND power_w<'Infinity'::float8),0)::float8 AS power_w,
 COALESCE(avg(temperature_c) FILTER(WHERE temperature_c>'-Infinity'::float8 AND temperature_c<'Infinity'::float8),0)::float8 AS temperature_c,
 count(*) FILTER(WHERE hashrate_hs>=0 AND hashrate_hs<'Infinity'::float8)>0 AS has_hashrate,
 count(*) FILTER(WHERE power_w>=0 AND power_w<'Infinity'::float8)>0 AS has_power,
 count(*) FILTER(WHERE temperature_c>'-Infinity'::float8 AND temperature_c<'Infinity'::float8)>0 AS has_temperature,
 COALESCE(bool_and(complete) FILTER (WHERE efficiency_candidate),false)::boolean AS complete,
 COALESCE(avg(CASE WHEN hashrate_hs>0 THEN 100.0 ELSE 0.0 END) FILTER(WHERE hashrate_hs>=0 AND hashrate_hs<'Infinity'::float8),0)::float8 AS uptime_percent
FROM samples
WHERE (sqlc.arg(site_filter)::bigint=0 OR site_id=sqlc.arg(site_filter)::bigint)
 AND (NOT sqlc.arg(policy_restricted)::boolean OR site_id=ANY(sqlc.arg(policy_sites)::bigint[]))
 AND NOT EXISTS (
  SELECT 1 FROM jsonb_to_recordset(sqlc.arg(host_authority)::jsonb) AS a(org_wide boolean,sites bigint[])
  WHERE NOT (CASE WHEN a.org_wide THEN site_id=0 OR NOT(site_id=ANY(a.sites)) ELSE site_id=ANY(a.sites) END)
 )
GROUP BY kind,asset_id,bucket
)
-- Reduce to the bounded chart shape before crossing the database boundary.
SELECT bucket,
 COALESCE(sum(hashrate_hs) FILTER(WHERE has_hashrate),0)::float8 AS hashrate_hs,
 COALESCE(sum(power_w) FILTER(WHERE has_power),0)::float8 AS power_w,
 COALESCE(avg(temperature_c) FILTER(WHERE has_temperature),0)::float8 AS temperature_c,
 count(*) FILTER(WHERE has_hashrate)::bigint AS hashrate_reporting_count,
 count(*) FILTER(WHERE has_power)::bigint AS power_reporting_count,
 count(*) FILTER(WHERE has_temperature)::bigint AS temperature_reporting_count,
 count(*) FILTER(WHERE has_hashrate AND has_power)::bigint AS paired_reporting_count,
 COALESCE(sum(hashrate_hs) FILTER(WHERE has_hashrate AND has_power),0)::float8 AS paired_hashrate_hs,
 COALESCE(sum(power_w) FILTER(WHERE has_hashrate AND has_power),0)::float8 AS paired_power_w,
 count(*) FILTER(WHERE has_hashrate AND hashrate_hs>0)::bigint AS hashing_count,
 count(*) FILTER(WHERE has_hashrate AND hashrate_hs=0)::bigint AS offline_count,
 count(*) FILTER(WHERE kind=6 AND NOT complete)::bigint AS partial_module_count,
 bool_and(complete)::boolean AS complete,
 COALESCE(avg(uptime_percent) FILTER(WHERE has_hashrate),0)::float8 AS uptime_percent
FROM asset_buckets GROUP BY bucket ORDER BY bucket;

-- name: ListHostedAccountHistoricalSites :many
SELECT id,name FROM site WHERE org_id=$1 AND id=ANY(sqlc.arg(site_ids)::bigint[]);
