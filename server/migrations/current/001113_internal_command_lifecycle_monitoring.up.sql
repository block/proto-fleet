-- requires-shared: 1000
ALTER TABLE queue_message ADD COLUMN processing_started_at timestamptz;

CREATE INDEX idx_queue_message_monitoring_pending ON queue_message(created_at,id)
 WHERE status IN ('PENDING','PROCESSING');

-- Queued and outstanding age use original admission. Running age uses the
-- start of the current attempt. Rows already running at migration have no start.
CREATE VIEW command_backlog_summary AS
WITH backlog AS (
 SELECT d.org_id,
 CASE WHEN q.command_type='FirmwareUpdate' THEN 'firmware' ELSE 'control' END AS command_class,
 q.status,q.created_at,q.updated_at,q.processing_started_at
 FROM queue_message q JOIN device d ON d.id=q.device_id
 WHERE q.status IN ('PENDING','PROCESSING')
), scopes AS (
 SELECT id AS org_id,'control'::text AS command_class FROM organization
 UNION ALL SELECT id,'firmware'::text FROM organization
)
SELECT s.org_id,s.command_class,
 count(b.status) FILTER (WHERE b.status='PENDING')::bigint AS queued_count,
 count(b.status) FILTER (WHERE b.status='PROCESSING')::bigint AS running_count,
 count(b.status)::bigint AS outstanding_count,
 COALESCE(max(GREATEST(0,EXTRACT(EPOCH FROM CURRENT_TIMESTAMP-b.created_at))) FILTER (WHERE b.status='PENDING'),0)::double precision AS oldest_queued_age_seconds,
 COALESCE(max(GREATEST(0,EXTRACT(EPOCH FROM CURRENT_TIMESTAMP-b.processing_started_at))) FILTER (WHERE b.status='PROCESSING' AND b.processing_started_at IS NOT NULL),0)::double precision AS oldest_running_age_seconds,
 COALESCE(max(GREATEST(0,EXTRACT(EPOCH FROM CURRENT_TIMESTAMP-b.created_at))),0)::double precision AS oldest_outstanding_age_seconds,
 count(b.status) FILTER (WHERE b.created_at>CURRENT_TIMESTAMP OR b.updated_at>CURRENT_TIMESTAMP
     OR (b.status='PROCESSING' AND b.processing_started_at>CURRENT_TIMESTAMP))::bigint AS invalid_clock_count
FROM scopes s LEFT JOIN backlog b ON b.org_id=s.org_id AND b.command_class=s.command_class
GROUP BY s.org_id,s.command_class;

-- HA provisions the monitoring role before Fleet migrations run.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        GRANT SELECT ON command_backlog_summary TO grafana_ha_ro;
    END IF;
END $$;
