-- name: GetHostedAccountActivityPage :one
WITH account_types(event_type, description) AS (VALUES
 ('create_hosted_account','Created account'),
 ('rename_hosted_account','Renamed account'),
 ('configure_hosted_account','Updated account permissions'),
 ('configure_hosted_account_sites','Updated account site access'),
 ('set_hosted_account_membership','Updated team member role'),
 ('revoke_hosted_account_membership','Removed team member'),
 ('set_hosted_account_ancestor_grant','Granted ancestor access'),
 ('revoke_hosted_account_ancestor_grant','Revoked ancestor access'),
 ('create_hosted_account_invitation','Invited team member'),
 ('revoke_hosted_account_invitation','Revoked team invitation'),
 ('accept_hosted_account_invitation','Team member joined'),
 ('create_hosted_account_key','Created account API key'),
 ('revoke_hosted_account_key','Revoked account API key'),
 ('create_hosted_account_pool','Created pool configuration'),
 ('revise_hosted_account_pool','Revised pool configuration'),
 ('archive_hosted_account_pool','Archived pool configuration'),
 ('set_hosted_account_pool_profile','Updated incoming pool profile'),
 ('account_assets_assigned','Assigned mining assets')
), command_types(event_type, description) AS (VALUES
 ('reboot.completed','Rebooted miners'),
 ('start_mining.completed','Started mining'),
 ('stop_mining.completed','Stopped mining'),
 ('set_power_target.completed','Updated power target'),
 ('set_cooling_mode.completed','Updated cooling mode'),
 ('update_mining_pools.completed','Updated mining pools'),
 ('firmware_update.completed','Updated firmware'),
 ('curtail.completed','Started curtailment'),
 ('uncurtail.completed','Ended curtailment')
), account_events AS NOT MATERIALIZED (
 SELECT a.id,a.event_id,a.created_at,
  CASE WHEN a.event_type='account_assets_assigned' THEN 'fleet_management'
       WHEN a.event_type IN ('create_hosted_account_pool','revise_hosted_account_pool','archive_hosted_account_pool','set_hosted_account_pool_profile') THEN 'pool'
       ELSE 'auth' END AS event_category,
  CASE WHEN a.event_type='account_assets_assigned' AND a.metadata->>'intent'='ASSET_ASSIGNMENT_INTENT_RECALL' THEN 'account_assets_recalled' ELSE a.event_type END AS event_type,
  CASE WHEN a.event_type='account_assets_assigned' AND a.metadata->>'intent'='ASSET_ASSIGNMENT_INTENT_RECALL' THEN 'Recalled mining assets' ELSE t.description END AS description,
  CASE WHEN a.event_type='account_assets_assigned' THEN 'asset' ELSE 'account' END::text AS scope_type,
  CASE WHEN a.event_type='account_assets_assigned' THEN (a.metadata->>'asset_count')::integer ELSE 0 END AS scope_count,
  a.result, '{}'::jsonb AS metadata
 FROM activity_log a JOIN account_types t ON t.event_type=a.event_type
 WHERE a.organization_id=sqlc.arg(organization_id)
  AND (sqlc.arg(include_summary)::boolean OR sqlc.narg(cursor_time)::timestamptz IS NULL
       OR (a.created_at,a.id)<(sqlc.narg(cursor_time)::timestamptz,sqlc.arg(cursor_id)::bigint))
  AND (a.metadata->>'account_id'=sqlc.arg(account_uuid)::text
       OR (a.event_type='account_assets_assigned' AND a.metadata->>'child_account_id'=sqlc.arg(account_uuid)::text))
  AND cardinality(sqlc.arg(site_ids)::bigint[])=0 AND NOT sqlc.arg(include_unassigned)::boolean
), miner_events AS NOT MATERIALIZED (
 SELECT a.id,a.event_id,a.created_at,'device_command'::text AS event_category,a.event_type,
  min(t.description)::text AS description,'device'::text AS scope_type,
  count(*)::integer AS scope_count,
  CASE WHEN bool_or(c.status='FAILED') THEN 'failure' ELSE 'success' END::text AS result,
  jsonb_build_object('success_count',count(*) FILTER(WHERE c.status='SUCCESS'),'failure_count',count(*) FILTER(WHERE c.status='FAILED')) AS metadata
 FROM activity_log a
 JOIN command_types t ON t.event_type=a.event_type
 JOIN command_batch_log b ON b.uuid=a.batch_id AND b.organization_id=a.organization_id
 JOIN command_on_device_log c ON c.command_batch_log_id=b.id AND c.org_id=a.organization_id
 WHERE a.organization_id=sqlc.arg(organization_id) AND a.event_category='device_command'
  AND (sqlc.arg(include_summary)::boolean OR sqlc.narg(cursor_time)::timestamptz IS NULL
       OR (a.created_at,a.id)<(sqlc.narg(cursor_time)::timestamptz,sqlc.arg(cursor_id)::bigint))
  AND EXISTS (
   SELECT 1 FROM hosted_account_asset_interval i
   WHERE i.organization_id=a.organization_id AND i.kind=5 AND i.device_id=c.device_id
    AND (i.client_id=sqlc.arg(account_internal_id)::bigint OR i.subclient_id=sqlc.arg(account_internal_id)::bigint)
    AND b.created_at>=i.started_at AND c.updated_at>=i.started_at AND a.created_at>=i.started_at
    AND (i.ended_at IS NULL OR (b.created_at<i.ended_at AND c.updated_at<i.ended_at AND a.created_at<i.ended_at))
    AND i.site_id IS NOT DISTINCT FROM c.site_id
    AND (NOT sqlc.arg(policy_restricted)::boolean OR i.site_id=ANY(sqlc.arg(policy_sites)::bigint[]))
    AND (cardinality(sqlc.arg(site_ids)::bigint[])=0 AND NOT sqlc.arg(include_unassigned)::boolean
         OR i.site_id=ANY(sqlc.arg(site_ids)::bigint[]) OR sqlc.arg(include_unassigned)::boolean AND i.site_id IS NULL)
    AND NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg(host_authority)::jsonb) AS h(org_wide boolean,sites bigint[])
     WHERE NOT (CASE WHEN h.org_wide THEN i.site_id IS NULL OR NOT(i.site_id=ANY(h.sites)) ELSE COALESCE(i.site_id,0)=ANY(h.sites) END)
    )
  )
 GROUP BY a.id,a.event_id,a.created_at,a.event_type
), base_safe AS NOT MATERIALIZED (
 SELECT * FROM account_events UNION ALL SELECT * FROM miner_events
), safe AS MATERIALIZED (
 SELECT * FROM base_safe WHERE sqlc.arg(include_summary)::boolean
), filtered AS NOT MATERIALIZED (
 SELECT * FROM (
  SELECT * FROM safe
  UNION ALL SELECT * FROM base_safe WHERE NOT sqlc.arg(include_summary)::boolean
 ) a
 WHERE (cardinality(sqlc.arg(categories)::text[])=0 OR a.event_category=ANY(sqlc.arg(categories)::text[]))
  AND (cardinality(sqlc.arg(event_types)::text[])=0 OR a.event_type=ANY(sqlc.arg(event_types)::text[]))
  AND (cardinality(sqlc.arg(scope_types)::text[])=0 OR a.scope_type=ANY(sqlc.arg(scope_types)::text[]))
  AND (sqlc.narg(start_time)::timestamptz IS NULL OR a.created_at>=sqlc.narg(start_time))
  AND (sqlc.narg(end_time)::timestamptz IS NULL OR a.created_at<=sqlc.narg(end_time))
  AND (sqlc.arg(search_text)::text='' OR strpos(lower(a.description || ' ' || replace(a.event_type,'_',' ')),lower(sqlc.arg(search_text)::text))>0)
), page AS MATERIALIZED (
 SELECT * FROM filtered a
 WHERE (sqlc.narg(cursor_time)::timestamptz IS NULL OR (a.created_at,a.id)<(sqlc.narg(cursor_time)::timestamptz,sqlc.arg(cursor_id)::bigint))
 ORDER BY a.created_at DESC,a.id DESC LIMIT sqlc.arg(page_size)::integer
)
SELECT jsonb_build_object(
 'page',jsonb_build_object(
  'activities',COALESCE((SELECT jsonb_agg(jsonb_build_object(
    'eventId',event_id,'eventCategory',event_category,'eventType',event_type,'description',description,
    'createdAt',created_at,'scopeType',scope_type,'scopeCount',scope_count,
    'scopeLabel',CASE WHEN scope_type='account' THEN 'Account' ELSE '' END,
    'result',result,'metadata',metadata
   ) ORDER BY created_at DESC,id DESC) FROM page),'[]'::jsonb),
  'totalCount',CASE WHEN sqlc.arg(include_summary)::boolean THEN (SELECT count(*) FROM filtered) ELSE 0 END,
  'filterOptions',CASE WHEN sqlc.arg(include_summary)::boolean THEN jsonb_build_object(
   'eventTypes',COALESCE((SELECT jsonb_agg(jsonb_build_object('eventType',event_type,'eventCategory',event_category) ORDER BY event_type) FROM (SELECT DISTINCT event_type,event_category FROM safe) types),'[]'::jsonb),
   'scopeTypes',COALESCE((SELECT jsonb_agg(scope_type ORDER BY scope_type) FROM (SELECT DISTINCT scope_type FROM safe) scopes),'[]'::jsonb)
  ) ELSE NULL END
 ),
 'cursor_id',COALESCE((SELECT id FROM page ORDER BY created_at,id LIMIT 1),0),
 'cursor_time',(SELECT created_at FROM page ORDER BY created_at,id LIMIT 1)
)::jsonb AS payload;
