-- Drop the one-non-terminal-event-per-org invariant. Curtailment now supports
-- multiple concurrent non-terminal events per org, each scoped to a disjoint
-- set of devices (e.g. independent per-site curtailment). Device-level
-- non-overlap is enforced instead by the partial unique index added in 000073.
--
-- CONCURRENTLY so the drop doesn't block on readers holding the index. Must be
-- the sole statement in the file (golang-migrate v4's postgres driver runs the
-- body with no implicit transaction).
DROP INDEX CONCURRENTLY IF EXISTS uq_curtailment_event_one_non_terminal_per_org;
