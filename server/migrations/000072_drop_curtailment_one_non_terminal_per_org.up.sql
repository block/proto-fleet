-- Drop the one-non-terminal-event-per-org invariant. Curtailment now supports
-- multiple concurrent non-terminal events per org, each scoped to a disjoint
-- device set (e.g. per-site); device-level non-overlap is enforced instead by
-- the partial unique index added in 000073. CONCURRENTLY (sole statement, no
-- implicit tx) so the drop doesn't block on readers holding the index.
DROP INDEX CONCURRENTLY IF EXISTS uq_curtailment_event_one_non_terminal_per_org;
