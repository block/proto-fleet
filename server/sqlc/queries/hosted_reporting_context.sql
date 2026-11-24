-- name: ReportingContextSize :one
-- Timestamp indexes are conservative candidates. Original typed intervals remain
-- authoritative in calculation; no stored value or interval is clipped.
WITH base AS NOT MATERIALIZED (
 SELECT *,
  (extract(epoch FROM regexp_replace(payload->>'end', '[.][0-9]+', '')::timestamptz)+COALESCE(('0.'||substring(payload->>'end' FROM '[.]([0-9]+)'))::numeric,0)) AS exact_end,
  (extract(epoch FROM regexp_replace(payload->>'receivedAt', '[.][0-9]+', '')::timestamptz)+COALESCE(('0.'||substring(payload->>'receivedAt' FROM '[.]([0-9]+)'))::numeric,0)) AS exact_received
 FROM hosted_reporting_fact
 WHERE organization_id=sqlc.arg(organization_id) AND account_id=sqlc.arg(account_id)
), window_facts AS MATERIALIZED (
 SELECT row_id FROM base
 WHERE kind IN ('order','coverage','module_coverage','meter','fee','incident','hashrate','obligation','external_result')
 AND start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
 AND (start_seconds,start_nanos)<(sqlc.arg(end_seconds)::bigint,sqlc.arg(end_nanos)::bigint)
 AND (end_at IS NULL OR (end_at>=sqlc.arg(window_start)::timestamptz-interval '1 microsecond' AND exact_end>(sqlc.arg(window_seconds)::bigint::numeric+sqlc.arg(window_nanos)::bigint::numeric/1000000000)))
 AND (received_at IS NULL OR received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz))
), relevant_episodes AS MATERIALIZED (
 SELECT DISTINCT asset_kind,asset_id,episode_id FROM base WHERE kind='obligation'
 AND (row_id IN (SELECT row_id FROM window_facts) OR
  (start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
   AND received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz)
   AND payload->>'severity'='S3' AND payload->>'ticketRef'<>''
   AND (COALESCE(payload->>'closedAt','0001-01-01T00:00:00Z')='0001-01-01T00:00:00Z'
     OR (payload->>'closedAt')::timestamptz>=sqlc.arg(period_end)::timestamptz-interval '1 microsecond')))
), episode_support AS MATERIALIZED (
 SELECT f.row_id FROM relevant_episodes e JOIN LATERAL (
  -- Keep rounded timestamp ties: exact Go reduction determines latest state.
  SELECT row_id FROM base f WHERE f.kind='obligation' AND f.asset_kind=e.asset_kind
  AND f.asset_id=e.asset_id AND f.episode_id=e.episode_id
  AND f.received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND f.exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz)
  AND (f.start_at=(SELECT min(x.start_at) FROM base x WHERE x.kind='obligation'
    AND x.asset_kind=e.asset_kind AND x.asset_id=e.asset_id AND x.episode_id=e.episode_id
    AND x.received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND x.exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz))
   OR f.start_at=(SELECT max(x.start_at) FROM base x WHERE x.kind='obligation'
    AND x.asset_kind=e.asset_kind AND x.asset_id=e.asset_id AND x.episode_id=e.episode_id
    AND x.received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND x.exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz)
    AND x.start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
    AND (x.start_seconds,x.start_nanos)<(sqlc.arg(end_seconds)::bigint,sqlc.arg(end_nanos)::bigint)))
 ) f ON true
), primary_facts AS MATERIALIZED (
 SELECT row_id FROM window_facts UNION SELECT row_id FROM episode_support
), meters AS MATERIALIZED (
 SELECT f.row_id FROM base m JOIN primary_facts p ON p.row_id=m.row_id
 JOIN base f ON f.kind='meter' AND f.meter_id=m.meter_id
 AND f.start_at<=m.end_at+interval '1 microsecond' AND f.end_at>=m.start_at-interval '1 microsecond'
 WHERE m.kind='meter'
), modules AS MATERIALIZED (
 SELECT f.row_id FROM base o JOIN primary_facts p ON p.row_id=o.row_id
 JOIN base f ON f.kind='module_coverage' AND f.asset_id=o.asset_id AND o.asset_kind=6
 AND f.start_at<=COALESCE(o.end_at,sqlc.arg(as_of)::timestamptz)+interval '1 microsecond'
 AND f.end_at>=o.start_at-interval '1 microsecond'
 WHERE o.kind='obligation'
 UNION SELECT f.row_id FROM base f JOIN primary_facts p ON p.row_id=f.row_id WHERE f.kind='module_coverage'
), coverage_support AS MATERIALIZED (
 SELECT f.row_id FROM base h JOIN primary_facts p ON p.row_id=h.row_id
 JOIN base f ON f.kind='coverage' AND f.asset_kind=h.asset_kind AND f.asset_id=h.asset_id
 AND f.start_at<=COALESCE(h.end_at,sqlc.arg(as_of)::timestamptz)+interval '1 microsecond'
 AND (f.end_at IS NULL OR f.end_at>=h.start_at-interval '1 microsecond')
 WHERE h.kind IN ('hashrate','obligation')
 UNION SELECT c.row_id FROM base m JOIN modules p ON p.row_id=m.row_id
 JOIN base c ON c.kind='coverage' AND c.fact_id=ANY(m.board_coverage_ids)
), active_coverage AS MATERIALIZED (
 SELECT f.* FROM base f WHERE kind='coverage'
 AND start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
 AND (start_seconds,start_nanos)<(sqlc.arg(end_seconds)::bigint,sqlc.arg(end_nanos)::bigint)
 AND (end_at IS NULL OR (end_at>=sqlc.arg(period_end)::timestamptz-interval '1 microsecond' AND f.exact_end>=(sqlc.arg(end_seconds)::bigint::numeric+sqlc.arg(end_nanos)::bigint::numeric/1000000000)))
), pool AS MATERIALIZED (
 SELECT f.row_id FROM active_coverage c JOIN LATERAL (
  SELECT row_id FROM base f WHERE f.kind='pool_observation' AND f.asset_kind=c.asset_kind AND f.asset_id=c.asset_id
  AND f.received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND f.exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz)
  AND f.start_at=(SELECT max(x.start_at) FROM base x WHERE x.kind='pool_observation'
    AND x.asset_kind=c.asset_kind AND x.asset_id=c.asset_id
    AND x.start_at>=c.start_at-interval '1 microsecond'
    AND (x.start_seconds,x.start_nanos)>=(c.start_seconds,c.start_nanos)
    AND x.start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
    AND (x.start_seconds,x.start_nanos)<(sqlc.arg(end_seconds)::bigint,sqlc.arg(end_nanos)::bigint)
    AND (c.end_at IS NULL OR (x.start_at<=c.end_at+interval '1 microsecond' AND (x.start_seconds::numeric+x.start_nanos::numeric/1000000000)<c.exact_end))
    AND x.received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND x.exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz))
 ) f ON true
), selected_without_orders AS MATERIALIZED (
 SELECT row_id FROM primary_facts UNION SELECT row_id FROM meters UNION SELECT row_id FROM modules
 UNION SELECT row_id FROM coverage_support UNION SELECT row_id FROM pool
), selected AS MATERIALIZED (
 SELECT row_id FROM selected_without_orders
 UNION SELECT o.row_id FROM base f JOIN selected_without_orders s ON s.row_id=f.row_id
 JOIN base o ON o.kind='order' AND o.fact_id=f.order_id
)
SELECT count(*)::bigint AS fact_count,COALESCE(sum(f.payload_bytes),0)::bigint AS payload_bytes,
 COALESCE(sum(octet_length(f.fact_id)),0)::bigint AS identity_bytes
FROM base f JOIN selected s ON s.row_id=f.row_id;

-- name: ListReportingContextMetadata :many
-- Timestamp indexes are conservative candidates. Original typed intervals remain
-- authoritative in calculation; no stored value or interval is clipped.
WITH base AS NOT MATERIALIZED (
 SELECT *,
  (extract(epoch FROM regexp_replace(payload->>'end', '[.][0-9]+', '')::timestamptz)+COALESCE(('0.'||substring(payload->>'end' FROM '[.]([0-9]+)'))::numeric,0)) AS exact_end,
  (extract(epoch FROM regexp_replace(payload->>'receivedAt', '[.][0-9]+', '')::timestamptz)+COALESCE(('0.'||substring(payload->>'receivedAt' FROM '[.]([0-9]+)'))::numeric,0)) AS exact_received
 FROM hosted_reporting_fact
 WHERE organization_id=sqlc.arg(organization_id) AND account_id=sqlc.arg(account_id)
), window_facts AS MATERIALIZED (
 SELECT row_id FROM base
 WHERE kind IN ('order','coverage','module_coverage','meter','fee','incident','hashrate','obligation','external_result')
 AND start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
 AND (start_seconds,start_nanos)<(sqlc.arg(end_seconds)::bigint,sqlc.arg(end_nanos)::bigint)
 AND (end_at IS NULL OR (end_at>=sqlc.arg(window_start)::timestamptz-interval '1 microsecond' AND exact_end>(sqlc.arg(window_seconds)::bigint::numeric+sqlc.arg(window_nanos)::bigint::numeric/1000000000)))
 AND (received_at IS NULL OR received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz))
), relevant_episodes AS MATERIALIZED (
 SELECT DISTINCT asset_kind,asset_id,episode_id FROM base WHERE kind='obligation'
 AND (row_id IN (SELECT row_id FROM window_facts) OR
  (start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
   AND received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz)
   AND payload->>'severity'='S3' AND payload->>'ticketRef'<>''
   AND (COALESCE(payload->>'closedAt','0001-01-01T00:00:00Z')='0001-01-01T00:00:00Z'
     OR (payload->>'closedAt')::timestamptz>=sqlc.arg(period_end)::timestamptz-interval '1 microsecond')))
), episode_support AS MATERIALIZED (
 SELECT f.row_id FROM relevant_episodes e JOIN LATERAL (
  -- Keep rounded timestamp ties: exact Go reduction determines latest state.
  SELECT row_id FROM base f WHERE f.kind='obligation' AND f.asset_kind=e.asset_kind
  AND f.asset_id=e.asset_id AND f.episode_id=e.episode_id
  AND f.received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND f.exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz)
  AND (f.start_at=(SELECT min(x.start_at) FROM base x WHERE x.kind='obligation'
    AND x.asset_kind=e.asset_kind AND x.asset_id=e.asset_id AND x.episode_id=e.episode_id
    AND x.received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND x.exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz))
   OR f.start_at=(SELECT max(x.start_at) FROM base x WHERE x.kind='obligation'
    AND x.asset_kind=e.asset_kind AND x.asset_id=e.asset_id AND x.episode_id=e.episode_id
    AND x.received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND x.exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz)
    AND x.start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
    AND (x.start_seconds,x.start_nanos)<(sqlc.arg(end_seconds)::bigint,sqlc.arg(end_nanos)::bigint)))
 ) f ON true
), primary_facts AS MATERIALIZED (
 SELECT row_id FROM window_facts UNION SELECT row_id FROM episode_support
), meters AS MATERIALIZED (
 SELECT f.row_id FROM base m JOIN primary_facts p ON p.row_id=m.row_id
 JOIN base f ON f.kind='meter' AND f.meter_id=m.meter_id
 AND f.start_at<=m.end_at+interval '1 microsecond' AND f.end_at>=m.start_at-interval '1 microsecond'
 WHERE m.kind='meter'
), modules AS MATERIALIZED (
 SELECT f.row_id FROM base o JOIN primary_facts p ON p.row_id=o.row_id
 JOIN base f ON f.kind='module_coverage' AND f.asset_id=o.asset_id AND o.asset_kind=6
 AND f.start_at<=COALESCE(o.end_at,sqlc.arg(as_of)::timestamptz)+interval '1 microsecond'
 AND f.end_at>=o.start_at-interval '1 microsecond'
 WHERE o.kind='obligation'
 UNION SELECT f.row_id FROM base f JOIN primary_facts p ON p.row_id=f.row_id WHERE f.kind='module_coverage'
), coverage_support AS MATERIALIZED (
 SELECT f.row_id FROM base h JOIN primary_facts p ON p.row_id=h.row_id
 JOIN base f ON f.kind='coverage' AND f.asset_kind=h.asset_kind AND f.asset_id=h.asset_id
 AND f.start_at<=COALESCE(h.end_at,sqlc.arg(as_of)::timestamptz)+interval '1 microsecond'
 AND (f.end_at IS NULL OR f.end_at>=h.start_at-interval '1 microsecond')
 WHERE h.kind IN ('hashrate','obligation')
 UNION SELECT c.row_id FROM base m JOIN modules p ON p.row_id=m.row_id
 JOIN base c ON c.kind='coverage' AND c.fact_id=ANY(m.board_coverage_ids)
), active_coverage AS MATERIALIZED (
 SELECT f.* FROM base f WHERE kind='coverage'
 AND start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
 AND (start_seconds,start_nanos)<(sqlc.arg(end_seconds)::bigint,sqlc.arg(end_nanos)::bigint)
 AND (end_at IS NULL OR (end_at>=sqlc.arg(period_end)::timestamptz-interval '1 microsecond' AND f.exact_end>=(sqlc.arg(end_seconds)::bigint::numeric+sqlc.arg(end_nanos)::bigint::numeric/1000000000)))
), pool AS MATERIALIZED (
 SELECT f.row_id FROM active_coverage c JOIN LATERAL (
  SELECT row_id FROM base f WHERE f.kind='pool_observation' AND f.asset_kind=c.asset_kind AND f.asset_id=c.asset_id
  AND f.received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND f.exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz)
  AND f.start_at=(SELECT max(x.start_at) FROM base x WHERE x.kind='pool_observation'
    AND x.asset_kind=c.asset_kind AND x.asset_id=c.asset_id
    AND x.start_at>=c.start_at-interval '1 microsecond'
    AND (x.start_seconds,x.start_nanos)>=(c.start_seconds,c.start_nanos)
    AND x.start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
    AND (x.start_seconds,x.start_nanos)<(sqlc.arg(end_seconds)::bigint,sqlc.arg(end_nanos)::bigint)
    AND (c.end_at IS NULL OR (x.start_at<=c.end_at+interval '1 microsecond' AND (x.start_seconds::numeric+x.start_nanos::numeric/1000000000)<c.exact_end))
    AND x.received_at<=sqlc.arg(as_of)::timestamptz+interval '1 microsecond' AND x.exact_received<=extract(epoch FROM sqlc.arg(as_of)::timestamptz))
 ) f ON true
), selected_without_orders AS MATERIALIZED (
 SELECT row_id FROM primary_facts UNION SELECT row_id FROM meters UNION SELECT row_id FROM modules
 UNION SELECT row_id FROM coverage_support UNION SELECT row_id FROM pool
), selected AS MATERIALIZED (
 SELECT row_id FROM selected_without_orders
 UNION SELECT o.row_id FROM base f JOIN selected_without_orders s ON s.row_id=f.row_id
 JOIN base o ON o.kind='order' AND o.fact_id=f.order_id
)
SELECT f.row_id,f.kind,f.fact_id,f.payload_bytes FROM base f JOIN selected s ON s.row_id=f.row_id
WHERE (f.kind,f.fact_id)>(sqlc.arg(after_kind)::text COLLATE "C",sqlc.arg(after_id)::text COLLATE "C")
ORDER BY f.kind,f.fact_id LIMIT sqlc.arg(page_limit)::bigint;

-- name: ReportingPeriodHasCoverage :one
SELECT EXISTS (
 SELECT 1 FROM hosted_reporting_fact f
 WHERE organization_id=sqlc.arg(organization_id) AND account_id=sqlc.arg(account_id) AND kind='coverage'
 AND start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
 AND (start_seconds,start_nanos)<(sqlc.arg(end_seconds)::bigint,sqlc.arg(end_nanos)::bigint)
 AND (end_at IS NULL OR (end_at>=sqlc.arg(period_start)::timestamptz-interval '1 microsecond'
  AND (extract(epoch FROM regexp_replace(payload->>'end','[.][0-9]+','')::timestamptz)
   +COALESCE(('0.'||substring(payload->>'end' FROM '[.]([0-9]+)'))::numeric,0))
   >(sqlc.arg(start_seconds)::bigint::numeric+sqlc.arg(start_nanos)::bigint::numeric/1000000000)))
)::boolean;
