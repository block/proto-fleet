-- name: GetHostedAccountActivityPage :one
WITH account_types AS (
 SELECT 'create_hosted_account'::text AS event_type,'auth'::text AS event_category,'account'::text AS scope_type,'Created account'::text AS description
 UNION ALL SELECT 'rename_hosted_account','auth','account','Renamed account'
 UNION ALL SELECT 'configure_hosted_account','auth','account','Updated account permissions'
 UNION ALL SELECT 'configure_hosted_account_sites','auth','account','Updated account site access'
 UNION ALL SELECT 'set_hosted_account_membership','auth','account','Updated team member role'
 UNION ALL SELECT 'revoke_hosted_account_membership','auth','account','Removed team member'
 UNION ALL SELECT 'set_hosted_account_ancestor_grant','auth','account','Granted ancestor access'
 UNION ALL SELECT 'revoke_hosted_account_ancestor_grant','auth','account','Revoked ancestor access'
 UNION ALL SELECT 'create_hosted_account_invitation','auth','account','Invited team member'
 UNION ALL SELECT 'revoke_hosted_account_invitation','auth','account','Revoked team invitation'
 UNION ALL SELECT 'accept_hosted_account_invitation','auth','account','Team member joined'
 UNION ALL SELECT 'create_hosted_account_key','auth','account','Created account API key'
 UNION ALL SELECT 'revoke_hosted_account_key','auth','account','Revoked account API key'
 UNION ALL SELECT 'create_hosted_account_pool','pool','account','Created pool configuration'
 UNION ALL SELECT 'revise_hosted_account_pool','pool','account','Revised pool configuration'
 UNION ALL SELECT 'archive_hosted_account_pool','pool','account','Archived pool configuration'
 UNION ALL SELECT 'set_hosted_account_pool_profile','pool','account','Updated incoming pool profile'
 UNION ALL SELECT 'account_assets_assigned','fleet_management','asset','Assigned mining assets'
 UNION ALL SELECT 'account_assets_recalled','fleet_management','asset','Recalled mining assets'
), command_types AS (
 SELECT 'reboot.completed'::text AS event_type,'Rebooted miners'::text AS description
 UNION ALL SELECT 'start_mining.completed','Started mining'
 UNION ALL SELECT 'stop_mining.completed','Stopped mining'
 UNION ALL SELECT 'set_power_target.completed','Updated power target'
 UNION ALL SELECT 'set_cooling_mode.completed','Updated cooling mode'
 UNION ALL SELECT 'update_mining_pools.completed','Updated mining pools'
 UNION ALL SELECT 'firmware_update.completed','Updated firmware'
 UNION ALL SELECT 'curtail.completed','Started curtailment'
 UNION ALL SELECT 'uncurtail.completed','Ended curtailment'
), account_events AS NOT MATERIALIZED (
 SELECT a.id,a.event_id,a.created_at,
  at.event_category,at.event_type,at.description,at.scope_type,
  CASE WHEN a.event_type='account_assets_assigned' THEN (a.metadata->>'asset_count')::integer ELSE 0 END AS scope_count,
  a.result, '{}'::jsonb AS metadata
 FROM activity_log a JOIN account_types at ON at.event_type=
  CASE WHEN a.event_type='account_assets_assigned' AND a.metadata->>'intent'='ASSET_ASSIGNMENT_INTENT_RECALL'
       THEN 'account_assets_recalled' ELSE a.event_type END
 WHERE a.organization_id=sqlc.arg(organization_id)
  AND (sqlc.arg(include_summary)::boolean OR sqlc.narg(cursor_time)::timestamptz IS NULL
       OR (a.created_at,a.id)<(sqlc.narg(cursor_time)::timestamptz,sqlc.arg(cursor_id)::bigint))
  AND (a.metadata->>'account_id'=sqlc.arg(account_uuid)::text
       OR (a.event_type='account_assets_assigned' AND a.metadata->>'child_account_id'=sqlc.arg(account_uuid)::text))
  AND cardinality(sqlc.arg(site_ids)::bigint[])=0 AND NOT sqlc.arg(include_unassigned)::boolean
), visible_commands AS NOT MATERIALIZED (
 -- Reuse this exact allocation/authority predicate for visibility and counts.
 SELECT a.id AS activity_id,c.status
 FROM activity_log a
 JOIN command_batch_log b ON b.uuid=a.batch_id AND b.organization_id=a.organization_id
 JOIN command_on_device_log c ON c.command_batch_log_id=b.id AND c.org_id=a.organization_id
 WHERE a.organization_id=sqlc.arg(organization_id)
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
), miner_events AS NOT MATERIALIZED (
 SELECT a.id,a.event_id,a.created_at,'device_command'::text AS event_category,a.event_type,
  t.description,'device'::text AS scope_type,0::integer AS scope_count,
  NULL::text AS result,'{}'::jsonb AS metadata
 FROM activity_log a JOIN command_types t ON t.event_type=a.event_type
 WHERE a.organization_id=sqlc.arg(organization_id) AND a.event_category='device_command'
  AND (sqlc.arg(include_summary)::boolean OR sqlc.narg(cursor_time)::timestamptz IS NULL
       OR (a.created_at,a.id)<(sqlc.narg(cursor_time)::timestamptz,sqlc.arg(cursor_id)::bigint))
  -- Keep visibility correlated so cursor reads can stop after the page rather
  -- than building a semi-join over every command in the organization.
  AND EXISTS (SELECT 1 FROM visible_commands c WHERE c.activity_id=a.id OFFSET 0)
), base_safe AS NOT MATERIALIZED (
 SELECT * FROM account_events UNION ALL SELECT * FROM miner_events
), safe AS MATERIALIZED (
 SELECT * FROM base_safe WHERE sqlc.arg(include_summary)::boolean
), filtered AS NOT MATERIALIZED (
 SELECT * FROM safe a
 WHERE (cardinality(sqlc.arg(categories)::text[])=0 OR a.event_category=ANY(sqlc.arg(categories)::text[]))
  AND (cardinality(sqlc.arg(event_types)::text[])=0 OR a.event_type=ANY(sqlc.arg(event_types)::text[]))
  AND (cardinality(sqlc.arg(scope_types)::text[])=0 OR a.scope_type=ANY(sqlc.arg(scope_types)::text[]))
  AND (sqlc.narg(start_time)::timestamptz IS NULL OR a.created_at>=sqlc.narg(start_time))
  AND (sqlc.narg(end_time)::timestamptz IS NULL OR a.created_at<=sqlc.narg(end_time))
  AND (sqlc.arg(search_text)::text='' OR strpos(lower(a.description || ' ' || replace(a.event_type,'_',' ')),lower(sqlc.arg(search_text)::text))>0)
), page AS MATERIALIZED (
 SELECT * FROM (
  (SELECT * FROM filtered a WHERE sqlc.arg(include_summary)::boolean
  AND (sqlc.narg(cursor_time)::timestamptz IS NULL OR (a.created_at,a.id)<(sqlc.narg(cursor_time)::timestamptz,sqlc.arg(cursor_id)::bigint))
   ORDER BY a.created_at DESC,a.id DESC LIMIT sqlc.arg(page_size)::integer)
  UNION ALL
  -- Cursor/export branches apply the same filters before their own limits,
  -- allowing the organization/time index to stop before historical commands.
  (SELECT * FROM account_events a
 WHERE (cardinality(sqlc.arg(categories)::text[])=0 OR a.event_category=ANY(sqlc.arg(categories)::text[]))
  AND (cardinality(sqlc.arg(event_types)::text[])=0 OR a.event_type=ANY(sqlc.arg(event_types)::text[]))
  AND (cardinality(sqlc.arg(scope_types)::text[])=0 OR a.scope_type=ANY(sqlc.arg(scope_types)::text[]))
  AND (sqlc.narg(start_time)::timestamptz IS NULL OR a.created_at>=sqlc.narg(start_time))
  AND (sqlc.narg(end_time)::timestamptz IS NULL OR a.created_at<=sqlc.narg(end_time))
  AND (sqlc.arg(search_text)::text='' OR strpos(lower(a.description || ' ' || replace(a.event_type,'_',' ')),lower(sqlc.arg(search_text)::text))>0)
  AND NOT sqlc.arg(include_summary)::boolean
  AND (sqlc.narg(cursor_time)::timestamptz IS NULL OR (a.created_at,a.id)<(sqlc.narg(cursor_time)::timestamptz,sqlc.arg(cursor_id)::bigint))
   ORDER BY a.created_at DESC,a.id DESC LIMIT sqlc.arg(page_size)::integer)
  UNION ALL
  (SELECT * FROM miner_events a
 WHERE (cardinality(sqlc.arg(categories)::text[])=0 OR a.event_category=ANY(sqlc.arg(categories)::text[]))
  AND (cardinality(sqlc.arg(event_types)::text[])=0 OR a.event_type=ANY(sqlc.arg(event_types)::text[]))
  AND (cardinality(sqlc.arg(scope_types)::text[])=0 OR a.scope_type=ANY(sqlc.arg(scope_types)::text[]))
  AND (sqlc.narg(start_time)::timestamptz IS NULL OR a.created_at>=sqlc.narg(start_time))
  AND (sqlc.narg(end_time)::timestamptz IS NULL OR a.created_at<=sqlc.narg(end_time))
  AND (sqlc.arg(search_text)::text='' OR strpos(lower(a.description || ' ' || replace(a.event_type,'_',' ')),lower(sqlc.arg(search_text)::text))>0)
  AND NOT sqlc.arg(include_summary)::boolean
  AND (sqlc.narg(cursor_time)::timestamptz IS NULL OR (a.created_at,a.id)<(sqlc.narg(cursor_time)::timestamptz,sqlc.arg(cursor_id)::bigint))
   ORDER BY a.created_at DESC,a.id DESC LIMIT sqlc.arg(page_size)::integer)
 ) candidates ORDER BY created_at DESC,id DESC LIMIT sqlc.arg(page_size)::integer
), projected_page AS (
 SELECT p.id,p.event_id,p.created_at,p.event_category,p.event_type,p.description,p.scope_type,
  CASE WHEN p.scope_type='device' THEN c.scope_count ELSE p.scope_count END AS scope_count,
  CASE WHEN p.scope_type='device' THEN c.result ELSE p.result END AS result,
  CASE WHEN p.scope_type='device' THEN c.metadata ELSE p.metadata END AS metadata
 FROM page p LEFT JOIN LATERAL (
  SELECT count(*)::integer AS scope_count,
   CASE WHEN bool_or(v.status='FAILED') THEN 'failure' ELSE 'success' END AS result,
   jsonb_build_object('success_count',count(*) FILTER(WHERE v.status='SUCCESS'),
                      'failure_count',count(*) FILTER(WHERE v.status='FAILED')) AS metadata
  FROM visible_commands v WHERE p.scope_type='device' AND v.activity_id=p.id
 ) c ON true
)
SELECT jsonb_build_object(
 'page',jsonb_build_object(
  'activities',COALESCE((SELECT jsonb_agg(jsonb_build_object(
    'eventId',event_id,'eventCategory',event_category,'eventType',event_type,'description',description,
    'createdAt',created_at,'scopeType',scope_type,'scopeCount',scope_count,
    'scopeLabel',CASE WHEN scope_type='account' THEN 'Account' ELSE '' END,
    'result',result,'metadata',metadata
   ) ORDER BY created_at DESC,id DESC) FROM projected_page),'[]'::jsonb),
  'totalCount',CASE WHEN sqlc.arg(include_summary)::boolean THEN (SELECT count(*) FROM filtered) ELSE 0 END,
  'filterOptions',CASE WHEN sqlc.arg(include_summary)::boolean THEN jsonb_build_object(
   'eventTypes',COALESCE((SELECT jsonb_agg(jsonb_build_object('eventType',event_type,'eventCategory',event_category) ORDER BY event_type) FROM (SELECT DISTINCT event_type,event_category FROM safe) types),'[]'::jsonb),
   'scopeTypes',COALESCE((SELECT jsonb_agg(scope_type ORDER BY scope_type) FROM (SELECT DISTINCT scope_type FROM safe) scopes),'[]'::jsonb)
  ) ELSE NULL END
 ),
 'cursor_id',COALESCE((SELECT id FROM page ORDER BY created_at,id LIMIT 1),0),
 'cursor_time',(SELECT created_at FROM page ORDER BY created_at,id LIMIT 1)
)::jsonb AS payload;
