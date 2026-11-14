-- name: CreateHostedReport :one
INSERT INTO hosted_report(report_id,organization_id,account_id,series_id,number,correction_of,correction_reason,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id,evidence_cutoff)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) RETURNING *;

-- name: GetHostedReport :one
SELECT * FROM hosted_report WHERE organization_id=$1 AND account_id=$2 AND report_id=$3;

-- name: LockHostedReport :one
SELECT * FROM hosted_report WHERE organization_id=$1 AND account_id=$2 AND report_id=$3 FOR UPDATE;

-- name: ListHostedReportSummaries :many
SELECT r.id,r.report_id,r.series_id,r.number,r.revision,r.input_revision,r.period_start,r.period,
 r.status,r.site_ids,r.created_by,r.owner_id,r.issued_at,
 r.correction_reason,r.incomplete_exception,
 COALESCE(previous.report_id,'')::text AS correction_of,
 COALESCE((r.result->>'complete')::boolean,false)::boolean AS complete,
 (COALESCE(r.result->'deadlines','{}'::jsonb) #- '{contract,sources}' #- '{internal,sources}')::jsonb AS deadlines,
 COALESCE(delivery.status,'')::text AS delivery_status
FROM hosted_report r
LEFT JOIN hosted_report previous ON previous.id=r.correction_of
LEFT JOIN LATERAL (
 SELECT d.status FROM hosted_report_delivery d WHERE d.report_internal_id=r.id
 ORDER BY (d.status='confirmed') DESC,d.id DESC LIMIT 1
) delivery ON true
WHERE r.organization_id=sqlc.arg(organization_id) AND r.account_id=sqlc.arg(account_id)
 AND (sqlc.arg(host)::boolean OR (r.status='issued' AND NOT EXISTS (
   SELECT 1 FROM hosted_report newer WHERE newer.organization_id=r.organization_id AND newer.account_id=r.account_id AND newer.series_id=r.series_id AND newer.status='issued' AND newer.number>r.number
 )))
 AND (sqlc.arg(series_id)::text='' OR r.series_id=sqlc.arg(series_id))
 AND (sqlc.arg(before_id)::bigint=0 OR (r.period_start,r.id)<(sqlc.arg(before_start)::timestamptz,sqlc.arg(before_id)::bigint))
 AND (sqlc.arg(kind)::text='' OR r.kind=sqlc.arg(kind))
 AND (sqlc.arg(search)::text='' OR strpos(lower(CASE r.kind WHEN 'monthly' THEN 'Monthly service report Official report' WHEN 'quarterly' THEN 'Quarterly service report Official report' WHEN 'annual' THEN 'Annual performance report Official report' ELSE 'Requested operational report Host-prepared response ad_hoc' END||' '||r.period::text||' '||to_char(r.period_start AT TIME ZONE r.timezone,'Mon YYYY')),lower(sqlc.arg(search)))>0)
ORDER BY r.period_start DESC,r.id DESC LIMIT sqlc.arg(page_limit)::int;

-- name: RegenerateHostedReport :one
UPDATE hosted_report SET period=$4,period_start=$5,period_end=$6,as_of=$7,timezone=$8,kind=$9,input_revision=$10,result=$11,site_ids=$12,created_by=$13,owner_id=$14,evidence_cutoff=$15,
 revision=revision+1,updated_at=clock_timestamp()
WHERE id=$1 AND revision=$2 AND status='draft' AND organization_id=$3 RETURNING *;

-- name: IssueHostedReport :one
UPDATE hosted_report SET status='issued',issued_at=$5,issued_by=$3,incomplete_exception=$4,revision=revision+1,updated_at=clock_timestamp()
WHERE id=$1 AND revision=$2 AND status='draft' RETURNING *;

-- name: SelectHostedReportEvidenceCutoff :one
-- Run as its own statement, outside the preparation transaction.
SELECT hosted_report_evidence_cutoff(sqlc.arg(as_of)::timestamptz)::timestamptz AS evidence_cutoff;

-- Sites of the report's monitoring evidence: allocated intervals of the account
-- with an hourly row inside the period and a coverage segment, captured by the
-- cutoff. Gap hours carry no observation and add no site. Stored coverage
-- timestamps only select candidates; overlap uses each segment's original bounds.
-- name: HostedReportEvidenceSites :many
SELECT DISTINCT a.site_id FROM hosted_report r
JOIN hosted_account_reporting_allocation_snapshot a ON a.organization_id=r.organization_id AND (a.client_id=r.account_id OR a.subclient_id=r.account_id)
CROSS JOIN LATERAL (
 SELECT c.start_seconds::numeric+c.start_nanos::numeric/1000000000 AS start_epoch,
  CASE WHEN c.end_at IS NOT NULL THEN extract(epoch FROM regexp_replace(c.payload->>'end','[.][0-9]+','')::timestamptz)+COALESCE(('0.'||substring(c.payload->>'end' FROM '[.]([0-9]+)'))::numeric,0) END AS end_epoch
 FROM hosted_reporting_fact c
 WHERE c.organization_id=r.organization_id AND c.account_id=r.account_id AND c.kind='coverage' AND c.allocation_interval_id=a.interval_id
  AND c.start_at<=r.period_end+INTERVAL '1 microsecond' AND (c.end_at IS NULL OR c.end_at>=r.period_start-INTERVAL '1 microsecond')) c
WHERE r.id=$1 AND c.start_epoch<extract(epoch FROM r.period_end) AND (c.end_epoch IS NULL OR c.end_epoch>extract(epoch FROM r.period_start))
 AND EXISTS(SELECT 1 FROM hosted_account_reporting_hour h
 WHERE h.interval_id=a.interval_id AND h.captured_at<=r.evidence_cutoff
 AND h.bucket>=time_bucket(INTERVAL '1 hour',r.period_start) AND h.bucket<r.period_end
 AND extract(epoch FROM h.bucket)+3600>c.start_epoch AND (c.end_epoch IS NULL OR extract(epoch FROM h.bucket)<c.end_epoch));

-- name: CreateHostedReportDelivery :one
INSERT INTO hosted_report_delivery(report_internal_id,request_key,channel,status,sent_at,received_at,source_ref,evidence,failure_category,actor_id)
SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10 WHERE EXISTS(SELECT 1 FROM hosted_report WHERE id=$1 AND status='issued')
ON CONFLICT DO NOTHING RETURNING *;

-- name: GetHostedReportDelivery :one
SELECT * FROM hosted_report_delivery WHERE report_internal_id=$1 AND request_key=$2;

-- name: ListHostedReportDeliveries :many
SELECT * FROM hosted_report_delivery WHERE report_internal_id=sqlc.arg(report_internal_id)
ORDER BY id DESC LIMIT sqlc.arg(page_limit)::int;

-- name: HostedReportDeliveryStatus :one
SELECT COALESCE((SELECT d.status FROM hosted_report_delivery d WHERE d.report_internal_id=$1
 ORDER BY (d.status='confirmed') DESC,d.id DESC LIMIT 1),'')::text AS delivery_status;

-- name: CreateHostedReportRequest :one
INSERT INTO hosted_report_request(request_id,organization_id,account_id,received_at,source_ref,requester,scope,owner_id,target_at,limitations,communication_ref,delivery_ref,actor_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: ListHostedReportRequests :many
SELECT r.id,r.request_id,r.received_at,r.source_ref,r.requester,r.scope,r.target_at,r.limitations,r.communication_ref,r.delivery_ref,
 u.user_id AS owner_user_id
FROM hosted_report_request r
JOIN "user" u ON u.id=r.owner_id
WHERE r.organization_id=sqlc.arg(organization_id) AND r.account_id=sqlc.arg(account_id)
 AND (sqlc.arg(before_id)::bigint=0 OR (r.received_at,r.id)<(sqlc.arg(before_received_at)::timestamptz,sqlc.arg(before_id)::bigint))
ORDER BY r.received_at DESC,r.id DESC LIMIT sqlc.arg(page_limit)::int;

-- name: NextHostedReportNumber :one
SELECT (COALESCE(max(number),0)+1)::bigint AS number FROM hosted_report WHERE organization_id=$1 AND account_id=$2 AND series_id=$3;

-- name: SetHostedReportContext :one
UPDATE hosted_report SET result=$2,site_ids=$3 WHERE id=$1 AND status='draft' RETURNING *;

-- name: HostedReportPublicID :one
SELECT report_id FROM hosted_report WHERE id=$1;

-- name: FindHostedOriginalReport :one
SELECT report_id FROM hosted_report
WHERE organization_id=$1 AND account_id=$2 AND kind=$3 AND period_start=$4 AND period_end=$5
 AND correction_of IS NULL AND report_id<>$6
ORDER BY id LIMIT 1;

-- name: HasIssuedHostedOriginalReport :one
SELECT EXISTS(SELECT 1 FROM hosted_report
 WHERE organization_id=$1 AND account_id=$2 AND kind=$3 AND period_start=$4 AND period_end=$5
 AND correction_of IS NULL AND status='issued');

-- name: ActiveHostedReportCorrection :one
SELECT * FROM hosted_report
WHERE organization_id=$1 AND account_id=$2 AND correction_of=$3 AND status='draft'
ORDER BY number,id LIMIT 1;
