-- Order-compatible index for ListCurtailmentEventsForOrg's org-scoped
-- id-desc cursor pagination. State filtering remains a residual predicate;
-- avoid a second index until production history volume proves it necessary.
--
-- Uses CONCURRENTLY so the build does not block writes on high-row-count
-- deploys. golang-migrate v4's postgres driver runs each statement directly
-- via conn.ExecContext — no implicit transaction wraps the migration body —
-- so CREATE INDEX CONCURRENTLY is safe here. If a partial build fails it
-- leaves schema_migrations.dirty=true at version 55 and may leave an
-- INVALID index in pg_indexes; operator recovery is to DROP the INVALID
-- index and `migrate force 54` before re-deploying.
CREATE INDEX CONCURRENTLY idx_curtailment_event_org_id_desc
    ON curtailment_event (org_id, id DESC);
