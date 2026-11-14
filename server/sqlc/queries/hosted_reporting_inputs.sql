-- name: GetReportingUser :one
SELECT u.id FROM "user" u JOIN user_organization o ON o.user_id=u.id
WHERE u.user_id=$1 AND o.organization_id=$2 AND u.deleted_at IS NULL AND o.deleted_at IS NULL;

-- name: GetReportingAuthority :one
SELECT * FROM hosted_reporting_authority WHERE organization_id=$1 AND account_id=$2 AND user_id=$3;

-- name: ListReportingAuthorities :many
SELECT a.*,u.user_id AS user_uuid,u.username FROM hosted_reporting_authority a JOIN "user" u ON u.id=a.user_id
WHERE a.organization_id=$1 AND a.account_id=$2 ORDER BY u.user_id;

-- name: SetReportingAuthority :one
INSERT INTO hosted_reporting_authority(organization_id,account_id,user_id,can_create,all_sites,site_ids)
SELECT sqlc.arg(organization_id),sqlc.arg(account_id),sqlc.arg(user_id),sqlc.arg(can_create),sqlc.arg(all_sites),sqlc.arg(site_ids)::bigint[]
WHERE sqlc.arg(expected_revision)::bigint=0 OR EXISTS(SELECT 1 FROM hosted_reporting_authority WHERE organization_id=sqlc.arg(organization_id) AND account_id=sqlc.arg(account_id) AND user_id=sqlc.arg(user_id))
ON CONFLICT(organization_id,account_id,user_id) DO UPDATE
SET can_create=EXCLUDED.can_create,all_sites=EXCLUDED.all_sites,site_ids=EXCLUDED.site_ids,
 revision=hosted_reporting_authority.revision+1,updated_at=clock_timestamp()
WHERE hosted_reporting_authority.revision=sqlc.arg(expected_revision)
RETURNING *;

-- name: GetReportingInputs :one
SELECT * FROM hosted_reporting_inputs WHERE organization_id=$1 AND account_id=$2;

-- name: LockReportingInputsForUpdate :one
SELECT * FROM hosted_reporting_inputs WHERE organization_id=$1 AND account_id=$2 FOR UPDATE;

-- name: LockReportingInputsForReport :one
SELECT * FROM hosted_reporting_inputs WHERE organization_id=$1 AND account_id=$2 FOR NO KEY UPDATE;

-- name: LockReportingInputsForRead :one
SELECT * FROM hosted_reporting_inputs WHERE organization_id=$1 AND account_id=$2 FOR KEY SHARE;

-- name: LockReportingInputsForShare :one
SELECT * FROM hosted_reporting_inputs WHERE organization_id=$1 AND account_id=$2 FOR SHARE;

-- name: InitializeReportingInputs :execrows
INSERT INTO hosted_reporting_inputs(organization_id,account_id,revision,updated_by,source_ref,configuration,site_ids)
VALUES($1,$2,1,$3,$4,$5,'{}') ON CONFLICT DO NOTHING;

-- name: UpdateReportingInputs :one
UPDATE hosted_reporting_inputs SET revision=sqlc.arg(revision),updated_by=sqlc.arg(updated_by),source_ref=sqlc.arg(source_ref),configuration=sqlc.arg(configuration),site_ids=sqlc.arg(site_ids),updated_at=clock_timestamp()
WHERE organization_id=sqlc.arg(organization_id) AND account_id=sqlc.arg(account_id) RETURNING *;

-- name: ListHostReportingAccounts :many
SELECT a.account_id,a.id,a.name,a.depth,COALESCE(g.can_create,false)::boolean AS can_create,
 COALESCE(g.all_sites,false)::boolean AS all_sites,COALESCE(g.site_ids,'{}'::bigint[])::bigint[] AS site_ids
FROM hosted_account a
JOIN hosted_account root ON root.organization_id=a.organization_id AND root.depth=0
LEFT JOIN hosted_account parent ON parent.organization_id=a.organization_id AND parent.id=a.parent_id
LEFT JOIN hosted_reporting_authority g ON g.organization_id=a.organization_id AND g.account_id=a.id AND g.user_id=$2
WHERE a.organization_id=$1 AND a.depth>0 AND NOT a.suspended AND NOT root.suspended AND NOT COALESCE(parent.suspended,false)
 AND (sqlc.arg(administrator)::boolean OR g.can_create)
ORDER BY a.name,a.account_id;

-- name: ListReportingCaptureStreams :many
-- Current allocations only; the status view also lists recently ended intervals.
SELECT interval_id,site_id,source_id,metric,has_observation,
 last_observation_captured_at, last_observation_source_newest_at
FROM hosted_account_reporting_capture_status
WHERE organization_id=$1 AND (client_id=$2 OR subclient_id=$2) AND allocation_ended_at IS NULL
ORDER BY interval_id,source_id,metric;

-- name: ListReportingAllocationIntervals :many
SELECT * FROM hosted_account_asset_interval
WHERE id=ANY(sqlc.arg(ids)::bigint[]) AND organization_id=sqlc.arg(organization_id)
 AND (client_id=sqlc.arg(client_id) OR subclient_id=sqlc.arg(client_id)) ORDER BY id;

-- name: ListReportingSites :many
SELECT id,deleted_at FROM site WHERE id=ANY(sqlc.arg(ids)::bigint[]) AND org_id=sqlc.arg(org_id) ORDER BY id;

-- name: LockReportingDevices :many
-- Historical evidence may reference moved or soft-deleted devices.
SELECT id FROM device WHERE id=ANY(sqlc.arg(ids)::bigint[]) AND org_id=sqlc.arg(org_id)
ORDER BY id FOR SHARE NOWAIT;

-- name: CountReportingContainers :one
WITH claims AS (
 SELECT (value->>'site_id')::bigint AS site_id,(value->>'container_id')::bigint AS container_id
 FROM jsonb_array_elements(sqlc.arg(claims)::jsonb)
)
SELECT count(*)::bigint FROM claims c JOIN device d ON d.id=c.container_id AND d.org_id=sqlc.arg(org_id)
WHERE d.site_id=c.site_id OR EXISTS(SELECT 1 FROM hosted_account_asset_interval i
 WHERE i.device_id=d.id AND i.organization_id=d.org_id AND i.site_id=c.site_id);

-- name: PutReportingFact :exec
INSERT INTO hosted_reporting_fact(organization_id,account_id,kind,fact_id,start_at,end_at,start_seconds,start_nanos,received_at,site_id,container_id,asset_kind,asset_id,order_id,meter_id,allocation_interval_id,episode_id,board_coverage_ids,payload)
VALUES(sqlc.arg(organization_id),sqlc.arg(account_id),sqlc.arg(kind),sqlc.arg(fact_id),sqlc.arg(start_at),sqlc.narg(end_at),sqlc.arg(start_seconds),sqlc.arg(start_nanos),sqlc.narg(received_at),sqlc.narg(site_id),sqlc.narg(container_id),sqlc.narg(asset_kind),sqlc.narg(asset_id),sqlc.narg(order_id),sqlc.narg(meter_id),sqlc.narg(allocation_interval_id),sqlc.narg(episode_id),sqlc.arg(board_coverage_ids)::text[],sqlc.arg(payload))
ON CONFLICT(organization_id,account_id,kind,fact_id) DO UPDATE SET
 start_at=EXCLUDED.start_at,end_at=EXCLUDED.end_at,start_seconds=EXCLUDED.start_seconds,start_nanos=EXCLUDED.start_nanos,received_at=EXCLUDED.received_at,
 site_id=EXCLUDED.site_id,container_id=EXCLUDED.container_id,asset_kind=EXCLUDED.asset_kind,asset_id=EXCLUDED.asset_id,order_id=EXCLUDED.order_id,meter_id=EXCLUDED.meter_id,
 allocation_interval_id=EXCLUDED.allocation_interval_id,episode_id=EXCLUDED.episode_id,board_coverage_ids=EXCLUDED.board_coverage_ids,payload=EXCLUDED.payload;

-- name: DeleteReportingFact :execrows
DELETE FROM hosted_reporting_fact WHERE organization_id=$1 AND account_id=$2 AND kind=$3 AND fact_id=$4;

-- name: GetReportingFact :one
SELECT * FROM hosted_reporting_fact WHERE organization_id=$1 AND account_id=$2 AND kind=$3 AND fact_id=$4;

-- name: ListReportingFactPageMetadata :many
SELECT row_id,fact_id,payload_bytes FROM hosted_reporting_fact
WHERE organization_id=$1 AND account_id=$2 AND kind=$3 AND fact_id>sqlc.arg(after_id)
ORDER BY fact_id LIMIT sqlc.arg(page_limit);

-- name: GetReportingFactPayloads :many
SELECT row_id,kind,fact_id,payload FROM hosted_reporting_fact
WHERE organization_id=$1 AND account_id=$2 AND row_id=ANY(sqlc.arg(row_ids)::bigint[]) ORDER BY row_id;

-- name: ListReportingReferenceMetadata :many
SELECT f.row_id,f.kind,f.fact_id,f.payload_bytes
FROM jsonb_to_recordset(sqlc.arg(keys)::jsonb) AS k(kind text,id text)
JOIN hosted_reporting_fact f ON f.kind=k.kind AND f.fact_id=k.id
WHERE f.organization_id=sqlc.arg(organization_id) AND f.account_id=sqlc.arg(account_id)
ORDER BY f.row_id;

-- name: CountReportingRegisters :many
SELECT kind,count(*)::bigint AS fact_count FROM hosted_reporting_fact
WHERE organization_id=$1 AND account_id=$2 GROUP BY kind ORDER BY kind;

-- name: AdjustReportingFactSite :execrows
INSERT INTO hosted_reporting_fact_site(organization_id,account_id,site_id,fact_count)
SELECT sqlc.arg(organization_id),sqlc.arg(account_id),sqlc.arg(site_id),greatest(sqlc.arg(delta)::bigint,0)
WHERE sqlc.arg(delta)::bigint>=0 OR EXISTS(SELECT 1 FROM hosted_reporting_fact_site WHERE organization_id=sqlc.arg(organization_id) AND account_id=sqlc.arg(account_id) AND site_id=sqlc.arg(site_id))
ON CONFLICT(organization_id,account_id,site_id) DO UPDATE SET fact_count=hosted_reporting_fact_site.fact_count+sqlc.arg(delta);

-- name: DeleteEmptyReportingFactSites :exec
DELETE FROM hosted_reporting_fact_site WHERE organization_id=$1 AND account_id=$2 AND fact_count=0;

-- name: ListReportingFactSites :many
SELECT site_id FROM hosted_reporting_fact_site WHERE organization_id=$1 AND account_id=$2 ORDER BY site_id;

-- name: ListReportingFactDevices :many
SELECT DISTINCT container_id::bigint FROM hosted_reporting_fact WHERE organization_id=$1 AND account_id=$2 AND container_id>sqlc.arg(after_id)::bigint ORDER BY container_id LIMIT sqlc.arg(page_limit);

-- name: ListReportingWitnessMetadata :many
SELECT f.row_id,f.kind,f.fact_id,f.payload_bytes,f.site_id,f.container_id,
 (f.kind='module_coverage' OR (f.kind='coverage' AND NOT EXISTS(
 SELECT 1 FROM hosted_account_asset_interval i WHERE i.id=f.allocation_interval_id AND i.organization_id=f.organization_id
 AND (i.client_id=f.account_id OR i.subclient_id=f.account_id)
 AND i.kind=f.asset_kind AND i.asset_id=f.asset_id AND i.device_id=f.container_id AND i.site_id=f.site_id
 AND f.start_at>i.started_at+interval '1 microsecond'
 AND (i.ended_at IS NULL OR (f.end_at IS NOT NULL AND f.end_at<i.ended_at-interval '1 microsecond')))))::boolean AS needs_history
FROM hosted_reporting_fact f
WHERE f.organization_id=$1 AND f.account_id=$2 AND site_id IS NOT NULL AND row_id>sqlc.arg(after_row_id)::bigint ORDER BY row_id LIMIT sqlc.arg(page_limit);

-- name: ListReportingValidationMetadata :many
WITH changed AS (
 SELECT DISTINCT f.row_id FROM hosted_reporting_fact f
 WHERE f.organization_id=sqlc.arg(organization_id) AND f.account_id=sqlc.arg(account_id)
 AND f.row_id>sqlc.arg(after_row_id)::bigint AND f.row_id=ANY(sqlc.arg(changed_rows)::bigint[])
 ORDER BY f.row_id LIMIT sqlc.arg(page_limit)::integer
),
orders AS (
 SELECT DISTINCT f.row_id FROM hosted_reporting_fact f
 WHERE f.organization_id=sqlc.arg(organization_id) AND f.account_id=sqlc.arg(account_id)
 AND f.row_id>sqlc.arg(after_row_id)::bigint AND f.order_id=ANY(sqlc.arg(order_ids)::text[])
 ORDER BY f.row_id LIMIT sqlc.arg(page_limit)::integer
),
boards AS (
 SELECT DISTINCT f.row_id FROM hosted_reporting_fact f
 WHERE f.organization_id=sqlc.arg(organization_id) AND f.account_id=sqlc.arg(account_id)
 AND f.row_id>sqlc.arg(after_row_id)::bigint AND f.board_coverage_ids && sqlc.arg(board_ids)::text[]
 ORDER BY f.row_id LIMIT sqlc.arg(page_limit)::integer
),
assets AS (
 SELECT DISTINCT f.row_id FROM jsonb_array_elements(sqlc.arg(assets)::jsonb) a
 JOIN hosted_reporting_fact f ON f.asset_kind=(a->>'kind')::integer AND f.asset_id=(a->>'id')::bigint
 WHERE f.organization_id=sqlc.arg(organization_id) AND f.account_id=sqlc.arg(account_id)
 AND f.row_id>sqlc.arg(after_row_id)::bigint
 ORDER BY f.row_id LIMIT sqlc.arg(page_limit)::integer
),
candidate AS (
 SELECT row_id FROM changed
 UNION
 SELECT row_id FROM orders
 UNION
 SELECT row_id FROM boards
 UNION
 SELECT row_id FROM assets
)
SELECT f.row_id,f.kind,f.fact_id,f.payload_bytes FROM hosted_reporting_fact f JOIN candidate c ON c.row_id=f.row_id
ORDER BY f.row_id LIMIT sqlc.arg(page_limit)::integer;

-- name: ListReportingAssetSupportMetadata :many
SELECT row_id,kind,fact_id,payload_bytes,start_seconds,start_nanos FROM hosted_reporting_fact
WHERE organization_id=sqlc.arg(organization_id) AND account_id=sqlc.arg(account_id)
 AND asset_kind=sqlc.arg(asset_kind) AND asset_id=sqlc.arg(asset_id)
 AND kind=ANY(sqlc.arg(kinds)::text[])
 AND start_seconds<=sqlc.arg(window_end_seconds)::bigint
 AND start_at<=sqlc.arg(window_end)::timestamptz+interval '1 microsecond'
 AND (end_at IS NULL OR end_at>=sqlc.arg(window_start)::timestamptz-interval '1 microsecond')
 AND (start_seconds,start_nanos,row_id)>(sqlc.arg(after_seconds)::bigint,sqlc.arg(after_nanos)::integer,sqlc.arg(after_row_id)::bigint)
ORDER BY start_seconds,start_nanos,row_id LIMIT sqlc.arg(page_limit);

-- name: ListReportingMeterSupportMetadata :many
SELECT row_id,kind,fact_id,payload_bytes FROM hosted_reporting_fact
WHERE organization_id=$1 AND account_id=$2 AND meter_id=$3 AND row_id>sqlc.arg(after_row_id)::bigint
 AND start_at<=sqlc.arg(window_end)::timestamptz+interval '1 microsecond'
 AND end_at>=sqlc.arg(window_start)::timestamptz-interval '1 microsecond'
ORDER BY row_id LIMIT sqlc.arg(page_limit);

-- name: HasReportingAllocatedMeters :one
SELECT EXISTS(SELECT 1 FROM hosted_reporting_fact WHERE organization_id=$1 AND account_id=$2 AND kind='meter' AND payload->>'allocationFraction'<>'')::boolean;

-- name: ListReportingModuleInstallationCandidates :many
-- Conservative microsecond candidates; exact typed windows decide validity.
SELECT i.* FROM hashboard_installation i JOIN hashboard b ON b.id=i.hashboard_id
WHERE b.org_id=sqlc.arg(organization_id) AND i.id>sqlc.arg(after_id)::bigint
 AND (i.hashboard_id=ANY(sqlc.arg(board_ids)::bigint[]) OR i.module_id=ANY(sqlc.arg(module_ids)::bigint[]))
 AND i.installed_at<=sqlc.arg(window_end)::timestamptz+interval '1 microsecond'
 AND (i.removed_at IS NULL OR i.removed_at>=sqlc.arg(window_start)::timestamptz-interval '1 microsecond')
ORDER BY i.id LIMIT sqlc.arg(page_limit);
