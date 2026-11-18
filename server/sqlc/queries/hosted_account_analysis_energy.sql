-- name: AccountAnalysisEnergyContextSize :one
WITH energy_facts AS MATERIALIZED (
 SELECT row_id,kind,fact_id,order_id,payload_bytes FROM hosted_reporting_fact f
 WHERE f.organization_id=sqlc.arg(organization_id) AND f.account_id=sqlc.arg(account_id)
 AND f.kind IN ('coverage','meter')
 AND (sqlc.arg(site_filter)::bigint=0 OR f.site_id=sqlc.arg(site_filter)::bigint)
 AND (NOT sqlc.arg(policy_restricted)::boolean OR f.site_id=ANY(sqlc.arg(policy_sites)::bigint[]))
 AND NOT EXISTS (
  SELECT 1 FROM jsonb_to_recordset(sqlc.arg(host_authority)::jsonb) AS a(org_wide boolean,sites bigint[])
  WHERE NOT (CASE WHEN a.org_wide THEN NOT(f.site_id=ANY(a.sites)) ELSE f.site_id=ANY(a.sites) END)
 )
 AND start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
 AND (start_seconds,start_nanos)<(sqlc.arg(end_seconds)::bigint,sqlc.arg(end_nanos)::bigint)
 AND (end_at IS NULL OR (end_at>=sqlc.arg(period_start)::timestamptz-interval '1 microsecond'
  AND (extract(epoch FROM regexp_replace(payload->>'end','[.][0-9]+','')::timestamptz)
   +COALESCE(('0.'||substring(payload->>'end' FROM '[.]([0-9]+)'))::numeric,0))
   >(sqlc.arg(start_seconds)::bigint::numeric+sqlc.arg(start_nanos)::bigint::numeric/1000000000)))
), selected AS (
 SELECT row_id,kind,fact_id,payload_bytes FROM energy_facts
 UNION SELECT o.row_id,o.kind,o.fact_id,o.payload_bytes FROM hosted_reporting_fact o
 WHERE o.organization_id=sqlc.arg(organization_id) AND o.account_id=sqlc.arg(account_id)
 AND o.kind='order' AND o.fact_id IN (SELECT order_id FROM energy_facts)
)
SELECT count(*)::bigint AS fact_count,COALESCE(sum(payload_bytes),0)::bigint AS payload_bytes,
 COALESCE(sum(octet_length(fact_id)),0)::bigint AS identity_bytes FROM selected;

-- name: ListAccountAnalysisEnergyContextMetadata :many
WITH energy_facts AS MATERIALIZED (
 SELECT row_id,kind,fact_id,order_id,payload_bytes FROM hosted_reporting_fact f
 WHERE f.organization_id=sqlc.arg(organization_id) AND f.account_id=sqlc.arg(account_id)
 AND f.kind IN ('coverage','meter')
 AND (sqlc.arg(site_filter)::bigint=0 OR f.site_id=sqlc.arg(site_filter)::bigint)
 AND (NOT sqlc.arg(policy_restricted)::boolean OR f.site_id=ANY(sqlc.arg(policy_sites)::bigint[]))
 AND NOT EXISTS (
  SELECT 1 FROM jsonb_to_recordset(sqlc.arg(host_authority)::jsonb) AS a(org_wide boolean,sites bigint[])
  WHERE NOT (CASE WHEN a.org_wide THEN NOT(f.site_id=ANY(a.sites)) ELSE f.site_id=ANY(a.sites) END)
 )
 AND start_at<=sqlc.arg(period_end)::timestamptz+interval '1 microsecond'
 AND (start_seconds,start_nanos)<(sqlc.arg(end_seconds)::bigint,sqlc.arg(end_nanos)::bigint)
 AND (end_at IS NULL OR (end_at>=sqlc.arg(period_start)::timestamptz-interval '1 microsecond'
  AND (extract(epoch FROM regexp_replace(payload->>'end','[.][0-9]+','')::timestamptz)
   +COALESCE(('0.'||substring(payload->>'end' FROM '[.]([0-9]+)'))::numeric,0))
   >(sqlc.arg(start_seconds)::bigint::numeric+sqlc.arg(start_nanos)::bigint::numeric/1000000000)))
), selected AS (
 SELECT row_id,kind,fact_id,payload_bytes FROM energy_facts
 UNION SELECT o.row_id,o.kind,o.fact_id,o.payload_bytes FROM hosted_reporting_fact o
 WHERE o.organization_id=sqlc.arg(organization_id) AND o.account_id=sqlc.arg(account_id)
 AND o.kind='order' AND o.fact_id IN (SELECT order_id FROM energy_facts)
)
SELECT row_id,kind,fact_id,payload_bytes FROM selected
WHERE (kind,fact_id)>(sqlc.arg(after_kind)::text COLLATE "C",sqlc.arg(after_id)::text COLLATE "C")
ORDER BY kind,fact_id LIMIT sqlc.arg(page_limit)::bigint;
