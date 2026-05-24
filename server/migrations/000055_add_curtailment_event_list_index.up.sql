-- Order-compatible index for ListCurtailmentEventsForOrg's org-scoped
-- id-desc cursor pagination. State filtering remains a residual predicate;
-- avoid a second index until production history volume proves it necessary.
--
-- Uses CONCURRENTLY so the build does not block writes on high-row-count
-- deploys. golang-migrate v4's postgres driver does not wrap migration
-- bodies in a transaction (ExecContext runs each statement directly), so
-- CONCURRENTLY works without an annotation.
CREATE INDEX CONCURRENTLY idx_curtailment_event_org_id_desc
    ON curtailment_event (org_id, id DESC);
