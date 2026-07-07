DROP VIEW IF EXISTS fleet_active_organization;
-- pg_stat_statements is intentionally left in place: run-fleet.sh's
-- apply_database_tuning step owns it in production and recreates it on every
-- run; dropping it here would only discard accumulated query stats.
