-- Drop the timescaledb_toolkit extension.
--
-- It was enabled in 000006_create_continuous_aggregates.up.sql for
-- time-weighted aggregates that were never wired up. The continuous aggregates
-- on main only use base TimescaleDB (time_bucket, add_continuous_aggregate_policy)
-- and standard SQL aggregates, so no objects depend on the extension.
DROP EXTENSION IF EXISTS timescaledb_toolkit;
