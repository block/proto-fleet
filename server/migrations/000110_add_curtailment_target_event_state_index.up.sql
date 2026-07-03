-- Covering index for the live target rollup on the active-events poll path
-- (ListActiveCurtailmentEvents): the per-event COUNT(*) FILTER (WHERE state
-- ...) lateral touches only (curtailment_event_id, state), so between write
-- bursts it can run as an index-only scan instead of a heap fetch across
-- fleet-sized target sets. Unfiltered because the rollup counts every state,
-- including terminal rows.
--
-- Uses CONCURRENTLY so the build does not block writes on high-row-count
-- deploys. golang-migrate v4's postgres driver runs each statement directly
-- via conn.ExecContext — no implicit transaction wraps the migration body —
-- so CREATE INDEX CONCURRENTLY is safe here (sole statement in the file).
-- If a partial build fails it leaves schema_migrations.dirty=true at version
-- 110 and may leave an INVALID index in pg_indexes; operator recovery is to
-- DROP the INVALID index and `migrate force 109` before re-deploying.
CREATE INDEX CONCURRENTLY idx_curtailment_target_event_state
    ON curtailment_target (curtailment_event_id, state);
