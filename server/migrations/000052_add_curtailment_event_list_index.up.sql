-- Order-compatible index for ListCurtailmentEventsForOrg's org-scoped
-- id-desc cursor pagination. State filtering remains a residual predicate;
-- avoid a second index until production history volume proves it necessary.
CREATE INDEX idx_curtailment_event_org_id_desc
    ON curtailment_event (org_id, id DESC);
