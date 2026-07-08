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

-- Slow-query surface for the system-monitoring dashboard. SECURITY DEFINER
-- (owned by the migration superuser) so grafana_ro can read normalized
-- statement stats for THIS database without pg_read_all_stats, which would
-- also expose cluster-wide pg_stat_activity query text. Execute is revoked
-- from PUBLIC; run-fleet.sh grants it to the Grafana role only while system
-- monitoring is enabled.
CREATE FUNCTION fleet_slow_statements()
RETURNS TABLE (
    query text,
    calls bigint,
    total_exec_time double precision,
    mean_exec_time double precision,
    max_exec_time double precision,
    rows bigint
)
LANGUAGE sql
SECURITY DEFINER
-- pg_temp is listed LAST on purpose: when it is omitted, PostgreSQL searches
-- it FIRST (even before pg_catalog) for relation names, so a caller with temp
-- privileges could shadow an unqualified reference below and run their SQL
-- under the definer's rights (CVE-2018-1058 class). Every relation here is
-- schema-qualified as a second layer of the same defense.
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT s.query, s.calls, s.total_exec_time, s.mean_exec_time,
           s.max_exec_time, s.rows
    FROM public.pg_stat_statements s
    WHERE s.dbid = (SELECT oid FROM pg_catalog.pg_database WHERE datname = current_database())
$$;

REVOKE EXECUTE ON FUNCTION fleet_slow_statements() FROM PUBLIC;
