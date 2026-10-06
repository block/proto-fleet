-- Covering partial index for the active curtailment unavailable-reason
-- rollup. The hot active-events poll path already has an index-only state
-- rollup over (curtailment_event_id, state); this keeps the extra reason
-- aggregate scoped to unavailable targets and avoids heap reads for
-- last_error reason buckets between write bursts.
--
-- Uses CONCURRENTLY so the build does not block writes on high-row-count
-- deploys. golang-migrate v4's postgres driver runs each statement directly
-- via conn.ExecContext — no implicit transaction wraps the migration body —
-- so CREATE INDEX CONCURRENTLY is safe here (sole statement in the file).
-- If a partial build fails it leaves schema_migrations.dirty=true at version
-- 112 and may leave an INVALID index in pg_indexes; operator recovery is to
-- DROP the INVALID index and `migrate force 111` before re-deploying.
CREATE INDEX CONCURRENTLY idx_curtailment_target_unavailable_reason
    ON curtailment_target (curtailment_event_id, last_error)
    WHERE state = 'unavailable';
