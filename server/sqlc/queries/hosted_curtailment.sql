-- name: CaptureHostedCurtailmentConfirmations :exec
-- Called in the same transaction as positive telemetry promotion. Exact physical
-- identity, phase, dispatch and timestamp guards exclude arbitrary state writes.
WITH observations AS (
 INSERT INTO hosted_curtailment_confirmation(organization_id,event_id,device_id,curtail_batch,phase,phase_batch,sample_at,confirmed_at)
 SELECT e.org_id,e.id,d.id,t.curtail_batch_uuid,u.phase,u.batch_uuid,u.sample_at,u.confirmed_at
 FROM curtailment_event e
 JOIN curtailment_target t ON t.curtailment_event_id=e.id
 JOIN device d ON d.org_id=e.org_id AND d.device_identifier=t.device_identifier AND d.deleted_at IS NULL
 JOIN jsonb_to_recordset(sqlc.arg(updates)::jsonb) AS u(
  device_database_id bigint,device_identifier text,phase text,batch_uuid text,sample_at timestamptz,confirmed_at timestamptz
 ) ON u.device_database_id=d.id AND u.device_identifier=d.device_identifier
 WHERE e.id=sqlc.arg(event_id) AND t.curtail_batch_uuid IS NOT NULL
  AND t.confirmed_at=u.confirmed_at
  AND u.sample_at>=d.created_at AND u.sample_at<=u.confirmed_at
  AND u.sample_at>=CASE WHEN u.phase='curtail' THEN t.curtail_dispatched_at ELSE t.restore_dispatched_at END
  AND ((u.phase='curtail' AND e.state IN ('pending','active') AND t.desired_state='curtailed'
    AND t.state='confirmed' AND t.curtail_batch_uuid=u.batch_uuid)
   OR (u.phase='restore' AND e.state IN ('pending','active','restoring') AND t.desired_state='active'
    AND t.state='resolved' AND t.restore_batch_uuid=u.batch_uuid))
 ON CONFLICT(organization_id,event_id,device_id,curtail_batch,phase,phase_batch) DO NOTHING
 RETURNING *
)
INSERT INTO hosted_curtailment_asset(confirmation_id,interval_id,installation_id,organization_id,client_id,subclient_id,site_id,kind,asset_id)
SELECT o.id,i.id,p.id,i.organization_id,i.client_id,i.subclient_id,i.site_id,i.kind,i.asset_id
FROM observations o
JOIN hosted_account_asset_interval i ON i.organization_id=o.organization_id AND i.device_id=o.device_id
 AND i.started_at<=o.sample_at AND (i.ended_at IS NULL OR i.ended_at>o.confirmed_at)
LEFT JOIN hashboard_installation p ON i.kind=7 AND p.hashboard_id=i.asset_id AND p.device_id=o.device_id
 AND p.site_id IS NOT DISTINCT FROM i.site_id AND p.installed_at<=o.sample_at
 AND (p.removed_at IS NULL OR p.removed_at>o.confirmed_at)
WHERE o.phase='curtail' AND (i.kind=5 OR (i.kind=7 AND p.id IS NOT NULL));

-- name: ListHostedCurtailmentWindows :many
-- Account attribution is fixed at curtail confirmation. Retained allocation and
-- installation ends can only narrow it; no current device membership is used.
SELECT c.id AS confirmation_id,c.evidence_id,a.interval_id,a.kind,a.asset_id,COALESCE(a.site_id,0)::bigint AS site_id,
 c.confirmed_at AS started_at,
 COALESCE(r.confirmed_at,'0001-01-01T00:00:00Z'::timestamptz)::timestamptz AS restored_at,
 COALESCE(LEAST(i.ended_at,p.removed_at,n.confirmed_at),'0001-01-01T00:00:00Z'::timestamptz)::timestamptz AS attribution_end
FROM hosted_curtailment_asset a
JOIN hosted_curtailment_confirmation c ON c.id=a.confirmation_id
JOIN hosted_account_asset_interval i ON i.id=a.interval_id
LEFT JOIN hashboard_installation p ON p.id=a.installation_id
LEFT JOIN LATERAL (
 SELECT min(x.confirmed_at)::timestamptz AS confirmed_at
 FROM hosted_curtailment_confirmation x
 WHERE x.organization_id=c.organization_id AND x.event_id=c.event_id AND x.device_id=c.device_id
  AND x.curtail_batch=c.curtail_batch AND x.phase='restore'
  AND x.confirmed_at>=c.confirmed_at AND x.confirmed_at<=sqlc.arg(as_of)::timestamptz
  AND x.captured_at<=sqlc.arg(as_of)::timestamptz
) r ON true
LEFT JOIN LATERAL (
 SELECT min(x.confirmed_at)::timestamptz AS confirmed_at
 FROM hosted_curtailment_confirmation x
 WHERE x.organization_id=c.organization_id AND x.device_id=c.device_id AND x.phase='curtail'
  AND x.confirmed_at>c.confirmed_at AND x.confirmed_at<=sqlc.arg(as_of)::timestamptz
  AND x.captured_at<=sqlc.arg(as_of)::timestamptz
) n ON true
WHERE a.organization_id=sqlc.arg(organization_id)
 AND (a.client_id=sqlc.arg(account_id)::bigint OR a.subclient_id=sqlc.arg(account_id)::bigint)
 AND c.confirmed_at<sqlc.arg(period_end)::timestamptz AND c.confirmed_at<=sqlc.arg(as_of)::timestamptz
 AND c.captured_at<=sqlc.arg(as_of)::timestamptz
 AND (r.confirmed_at IS NULL OR r.confirmed_at>sqlc.arg(period_start)::timestamptz)
 AND (LEAST(i.ended_at,p.removed_at,n.confirmed_at) IS NULL OR LEAST(i.ended_at,p.removed_at,n.confirmed_at)>sqlc.arg(period_start)::timestamptz)
ORDER BY c.confirmed_at,c.id,a.interval_id
LIMIT sqlc.arg(row_limit)::int;
