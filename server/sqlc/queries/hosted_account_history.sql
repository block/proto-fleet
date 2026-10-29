-- name: GetHostedAccountHistoryEpoch :one
SELECT started_at FROM hosted_account_history_epoch WHERE singleton;

-- name: ListHostedAccountHistoryIntervals :many
SELECT i.id,i.kind,i.asset_id,i.device_id,i.client_id,COALESCE(i.subclient_id,0)::bigint AS subclient_id,
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
-- Physical-site authority is shared with retained reads and evaluated once per
-- interval. Every raw source still requires that site and half-open ownership.
WITH intervals AS MATERIALIZED (
 SELECT interval_id::bigint AS interval_id,kind::integer AS kind,asset_id::bigint AS asset_id,
 device_identifier::text AS device_identifier,site_id::bigint AS site_id,
 started_at::timestamptz AS started_at,ended_at::timestamptz AS ended_at
 FROM hosted_history_authorized_intervals(sqlc.arg(intervals)::jsonb,sqlc.arg(site_filter)::bigint,
  sqlc.arg(policy_restricted)::boolean,sqlc.arg(policy_sites)::bigint[],sqlc.arg(host_authority)::jsonb)
), samples AS (
 SELECT i.kind,i.asset_id,m.time,COALESCE(m.site_id,0)::bigint AS site_id,
 m.hash_rate_hs AS hashrate_hs,m.power_w,m.temp_c AS temperature_c,
 COALESCE(m.hash_rate_hs>=0 AND m.hash_rate_hs<'Infinity'::float8 AND m.power_w>=0 AND m.power_w<'Infinity'::float8,false) AS complete, true AS efficiency_candidate,
 NULL::integer AS uptime_state
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
 metric.name IN ('hashrate','power') AS efficiency_candidate,
 NULL::integer AS uptime_state
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
 UNION ALL
 -- Module health is independent of numeric metric sources. Authorize the
 -- observed state, not today's inventory state or a positive hashrate.
 SELECT i.kind,i.asset_id,s.state_observed_at,COALESCE(s.site_id,0)::bigint,
 NULL::float8,NULL::float8,NULL::float8,NULL::boolean,false,
 hosted_module_uptime_state(s.state)::integer AS uptime_state
 FROM intervals i
 JOIN container_module_diagnostic_observation s ON i.kind=6 AND s.module_id=i.asset_id
  AND COALESCE(s.site_id,0)=i.site_id
  AND s.collected_at>=sqlc.arg(start_time)::timestamptz-INTERVAL '5 seconds'
  AND s.collected_at<sqlc.arg(end_time)::timestamptz+INTERVAL '2 minutes'
  AND s.collected_at>=i.started_at-INTERVAL '5 seconds'
  AND (i.ended_at IS NULL OR s.collected_at<i.ended_at+INTERVAL '2 minutes')
  AND s.state_observed_at>s.collected_at-INTERVAL '2 minutes'
  AND s.state_observed_at<=s.collected_at+INTERVAL '5 seconds'
  AND (s.attribution_since IS NULL OR s.state_observed_at>=s.attribution_since)
  AND s.state_observed_at>=i.started_at AND (i.ended_at IS NULL OR s.state_observed_at<i.ended_at)
  AND s.state_observed_at>=sqlc.arg(start_time)::timestamptz AND s.state_observed_at<sqlc.arg(end_time)::timestamptz
 UNION ALL
 SELECT i.kind,i.asset_id,s.time,COALESCE(s.site_id,0)::bigint,
 NULL::float8,NULL::float8,NULL::float8,NULL::boolean,false,s.state::integer
 FROM intervals i
 JOIN miner_state_snapshots s ON i.kind=5 AND s.device_identifier=i.device_identifier
  AND s.org_id=sqlc.arg(organization_id)::bigint AND COALESCE(s.site_id,0)=i.site_id
  AND s.time>=i.started_at AND (i.ended_at IS NULL OR s.time<i.ended_at)
  AND s.time>=sqlc.arg(start_time)::timestamptz AND s.time<sqlc.arg(end_time)::timestamptz
), asset_buckets AS (
SELECT kind,asset_id,date_bin(make_interval(secs=>sqlc.arg(bucket_seconds)::int),time,sqlc.arg(start_time)::timestamptz)::timestamptz AS bucket,
 COALESCE(avg(hashrate_hs) FILTER(WHERE hashrate_hs>=0 AND hashrate_hs<'Infinity'::float8),0)::float8 AS hashrate_hs,
 COALESCE(avg(power_w) FILTER(WHERE power_w>=0 AND power_w<'Infinity'::float8),0)::float8 AS power_w,
 COALESCE(avg(temperature_c) FILTER(WHERE temperature_c>'-Infinity'::float8 AND temperature_c<'Infinity'::float8),0)::float8 AS temperature_c,
 last(temperature_c,time) FILTER(WHERE temperature_c>'-Infinity'::float8 AND temperature_c<'Infinity'::float8) AS latest_temperature_c,
 last(uptime_state,time) FILTER(WHERE uptime_state IS NOT NULL) AS uptime_state,
 count(*) FILTER(WHERE hashrate_hs>=0 AND hashrate_hs<'Infinity'::float8)>0 AS has_hashrate,
 count(*) FILTER(WHERE power_w>=0 AND power_w<'Infinity'::float8)>0 AS has_power,
 count(*) FILTER(WHERE temperature_c>'-Infinity'::float8 AND temperature_c<'Infinity'::float8)>0 AS has_temperature,
 COALESCE(bool_and(complete) FILTER (WHERE efficiency_candidate),false)::boolean AS complete,
 COALESCE(avg(CASE WHEN hashrate_hs>0 THEN 100.0 ELSE 0.0 END) FILTER(WHERE hashrate_hs>=0 AND hashrate_hs<'Infinity'::float8),0)::float8 AS uptime_percent
FROM samples
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
 count(*) FILTER(WHERE latest_temperature_c<0)::int AS temperature_cold_count,
 count(*) FILTER(WHERE latest_temperature_c>=0 AND latest_temperature_c<70)::int AS temperature_ok_count,
 count(*) FILTER(WHERE latest_temperature_c>=70 AND latest_temperature_c<90)::int AS temperature_hot_count,
 count(*) FILTER(WHERE latest_temperature_c>=90)::int AS temperature_critical_count,
 count(*) FILTER(WHERE uptime_state=3)::int AS status_hashing_count,
 count(*) FILTER(WHERE uptime_state=2)::int AS status_broken_count,
 count(*) FILTER(WHERE uptime_state IN (0,1))::int AS status_not_hashing_count,
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
-- name: GetHostedAccountHistorySummaryEpoch :one
SELECT started_at FROM hosted_account_history_summary_epoch WHERE singleton;

-- name: GetHostedAccountHistoryWatermarks :one
SELECT hourly::timestamptz,daily::timestamptz
FROM hosted_account_history_watermarks(sqlc.arg(start_time)::timestamptz);

-- name: UseHostedAccountHistoryCustomPlan :exec
-- Generic plans misestimate the allocation array and retained/raw UNION branches.
-- Keep this setting local to the dashboard transaction.
SELECT set_config('plan_cache_mode','force_custom_plan',true);

-- name: ListHostedAccountRetainedReadings :many
-- Share interval-level physical-site authority with raw reads, then preserve
-- independent sums/counts and source windows before combining assets.
WITH intervals AS MATERIALIZED (
 SELECT interval_id::bigint AS interval_id,kind::integer AS kind,asset_id::bigint AS asset_id,
 device_identifier::text AS device_identifier,site_id::bigint AS site_id,
 started_at::timestamptz AS started_at,ended_at::timestamptz AS ended_at
 FROM hosted_history_authorized_intervals(sqlc.arg(intervals)::jsonb,sqlc.arg(site_filter)::bigint,
  sqlc.arg(policy_restricted)::boolean,sqlc.arg(policy_sites)::bigint[],sqlc.arg(host_authority)::jsonb)
), retained AS (
 SELECT h.* FROM hosted_account_history_hourly h
 WHERE sqlc.arg(hourly)::boolean
  AND h.bucket>=date_trunc('hour',sqlc.arg(start_time)::timestamptz)
  AND h.bucket<sqlc.arg(end_time)::timestamptz
  -- A constant array, unlike the join below, reaches the real-time tail.
  AND h.interval_id=ANY(sqlc.arg(interval_ids)::bigint[])
 UNION ALL
 SELECT h.* FROM hosted_account_history_daily h
 WHERE NOT sqlc.arg(hourly)::boolean
  AND h.bucket>=date_trunc('day',sqlc.arg(start_time)::timestamptz AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
  AND h.bucket<sqlc.arg(end_time)::timestamptz
  AND h.interval_id=ANY(sqlc.arg(interval_ids)::bigint[])
-- Both routes share authorization. Inline it so the custom plan prunes the
-- unused route without spooling or scanning the wide history twice.
), authorized AS NOT MATERIALIZED (
 SELECT i.kind,i.asset_id,h.*,
 h.hashrate_source_oldest_at>=i.started_at
  AND h.hashrate_source_oldest_at>=sqlc.arg(available_from)::timestamptz
  AND (i.ended_at IS NULL OR h.hashrate_source_newest_at<i.ended_at) AS hashrate_authorized,
 h.power_source_oldest_at>=i.started_at
  AND h.power_source_oldest_at>=sqlc.arg(available_from)::timestamptz
  AND (i.ended_at IS NULL OR h.power_source_newest_at<i.ended_at) AS power_authorized,
 h.temperature_source_oldest_at>=i.started_at
  AND h.temperature_source_oldest_at>=sqlc.arg(available_from)::timestamptz
  AND (i.ended_at IS NULL OR h.temperature_source_newest_at<i.ended_at) AS temperature_authorized,
 h.state_source_oldest_at>=i.started_at
  AND h.state_source_oldest_at>=sqlc.arg(available_from)::timestamptz
  AND (i.ended_at IS NULL OR h.state_source_newest_at<i.ended_at) AS state_authorized
 FROM intervals i JOIN retained h ON h.interval_id=i.interval_id
), asset_buckets AS (
 SELECT kind,asset_id,
 date_bin(make_interval(secs=>sqlc.arg(bucket_seconds)::int),bucket,sqlc.arg(start_time)::timestamptz)::timestamptz AS bucket,
 CASE WHEN hashrate_authorized THEN COALESCE(hashrate_sum/NULLIF(hashrate_count,0),0) ELSE 0 END::float8 AS hashrate_hs,
 CASE WHEN power_authorized THEN COALESCE(power_sum/NULLIF(power_count,0),0) ELSE 0 END::float8 AS power_w,
 CASE WHEN temperature_authorized THEN COALESCE(temperature_sum/NULLIF(temperature_count,0),0) ELSE 0 END::float8 AS temperature_c,
 CASE WHEN temperature_authorized AND temperature_count>0 THEN temperature_latest_value END AS latest_temperature_c,
 CASE WHEN state_authorized AND state_count>0 THEN state_latest_value END AS uptime_state,
 COALESCE(hashrate_authorized AND hashrate_count>0,false) AS has_hashrate,
 COALESCE(power_authorized AND power_count>0,false) AS has_power,
 COALESCE(temperature_authorized AND temperature_count>0,false) AS has_temperature,
 COALESCE(CASE WHEN hashrate_authorized AND power_authorized THEN hashrate_complete AND power_complete
  WHEN hashrate_authorized THEN hashrate_complete WHEN power_authorized THEN power_complete END,false)::boolean AS complete,
 CASE WHEN hashrate_authorized THEN COALESCE(100.0*hashrate_positive_count/NULLIF(hashrate_count,0),0) ELSE 0 END::float8 AS uptime_percent
 FROM authorized
 WHERE sqlc.arg(direct_buckets)::boolean AND (hashrate_authorized OR power_authorized OR temperature_authorized OR state_authorized)
 UNION ALL
 SELECT kind,asset_id,
 date_bin(make_interval(secs=>sqlc.arg(bucket_seconds)::int),bucket,sqlc.arg(start_time)::timestamptz)::timestamptz AS bucket,
 COALESCE(sum(hashrate_sum) FILTER(WHERE hashrate_authorized)/NULLIF(sum(hashrate_count) FILTER(WHERE hashrate_authorized),0),0)::float8 AS hashrate_hs,
 COALESCE(sum(power_sum) FILTER(WHERE power_authorized)/NULLIF(sum(power_count) FILTER(WHERE power_authorized),0),0)::float8 AS power_w,
 COALESCE(sum(temperature_sum) FILTER(WHERE temperature_authorized)/NULLIF(sum(temperature_count) FILTER(WHERE temperature_authorized),0),0)::float8 AS temperature_c,
 last(temperature_latest_value,temperature_latest_at) FILTER(WHERE temperature_authorized AND temperature_count>0) AS latest_temperature_c,
 last(state_latest_value,state_latest_at) FILTER(WHERE state_authorized AND state_count>0) AS uptime_state,
 COALESCE(sum(hashrate_count) FILTER(WHERE hashrate_authorized),0)>0 AS has_hashrate,
 COALESCE(sum(power_count) FILTER(WHERE power_authorized),0)>0 AS has_power,
 COALESCE(sum(temperature_count) FILTER(WHERE temperature_authorized),0)>0 AS has_temperature,
 COALESCE(bool_and(CASE
  WHEN hashrate_authorized AND power_authorized THEN hashrate_complete AND power_complete
  WHEN hashrate_authorized THEN hashrate_complete WHEN power_authorized THEN power_complete END),false)::boolean AS complete,
 COALESCE(100.0*sum(hashrate_positive_count) FILTER(WHERE hashrate_authorized)/NULLIF(sum(hashrate_count) FILTER(WHERE hashrate_authorized),0),0)::float8 AS uptime_percent
 FROM authorized
 WHERE NOT sqlc.arg(direct_buckets)::boolean AND (hashrate_authorized OR power_authorized OR temperature_authorized OR state_authorized)
 GROUP BY 3,kind,asset_id
)
-- Reduce to the bounded chart shape before crossing the database boundary.
SELECT bucket,
 COALESCE(sum(hashrate_hs) FILTER(WHERE has_hashrate),0)::float8 AS hashrate_hs,
 COALESCE(sum(power_w) FILTER(WHERE has_power),0)::float8 AS power_w,
 COALESCE(avg(temperature_c) FILTER(WHERE has_temperature),0)::float8 AS temperature_c,
 count(*) FILTER(WHERE has_hashrate)::bigint AS hashrate_reporting_count,
 count(*) FILTER(WHERE has_power)::bigint AS power_reporting_count,
 count(*) FILTER(WHERE has_temperature)::bigint AS temperature_reporting_count,
 count(*) FILTER(WHERE latest_temperature_c<0)::int AS temperature_cold_count,
 count(*) FILTER(WHERE latest_temperature_c>=0 AND latest_temperature_c<70)::int AS temperature_ok_count,
 count(*) FILTER(WHERE latest_temperature_c>=70 AND latest_temperature_c<90)::int AS temperature_hot_count,
 count(*) FILTER(WHERE latest_temperature_c>=90)::int AS temperature_critical_count,
 count(*) FILTER(WHERE uptime_state=3)::int AS status_hashing_count,
 count(*) FILTER(WHERE uptime_state=2)::int AS status_broken_count,
 count(*) FILTER(WHERE uptime_state IN (0,1))::int AS status_not_hashing_count,
 count(*) FILTER(WHERE has_hashrate AND has_power)::bigint AS paired_reporting_count,
 COALESCE(sum(hashrate_hs) FILTER(WHERE has_hashrate AND has_power),0)::float8 AS paired_hashrate_hs,
 COALESCE(sum(power_w) FILTER(WHERE has_hashrate AND has_power),0)::float8 AS paired_power_w,
 count(*) FILTER(WHERE has_hashrate AND hashrate_hs>0)::bigint AS hashing_count,
 count(*) FILTER(WHERE has_hashrate AND hashrate_hs=0)::bigint AS offline_count,
 count(*) FILTER(WHERE kind=6 AND NOT complete)::bigint AS partial_module_count,
 bool_and(complete)::boolean AS complete,
 COALESCE(avg(uptime_percent) FILTER(WHERE has_hashrate),0)::float8 AS uptime_percent
FROM asset_buckets GROUP BY bucket ORDER BY bucket;
