-- pg_stat_statements backs the system-monitoring slow-query dashboard.
-- Production already creates it via run-fleet.sh apply_database_tuning; this
-- covers dev stacks (which never run that script). CREATE succeeds even
-- before the library is preloaded — only view reads would error.
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- Live-organization presence for the proto-fleet-system alert rules: host
-- metrics carry no org label, so each rule CROSS JOINs this view to fan one
-- host condition out to per-org alert instances. An owner-privilege view so
-- grafana_ro never gets SELECT on organization (miner_auth_private_key).
-- id cast to text to match the notification_metric_sample organization_id
-- label. Precedent: fleet_pollable_device_presence (000096).
CREATE VIEW fleet_active_organization AS
SELECT id::text AS organization_id
FROM organization
WHERE deleted_at IS NULL;
