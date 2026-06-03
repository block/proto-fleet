-- Recreate the one-non-terminal-event-per-org invariant. This rollback fails if
-- multiple non-terminal events already exist for any org (the relaxed model
-- allows them); move the extra events to terminal states first.
CREATE UNIQUE INDEX uq_curtailment_event_one_non_terminal_per_org
    ON curtailment_event (org_id)
    WHERE state IN ('pending', 'active', 'restoring');
