-- Recreate the one-non-terminal-event-per-org invariant (CONCURRENTLY, sole
-- statement). This rollback fails if multiple non-terminal events already exist
-- for any org (the relaxed model allows them); move the extras to terminal
-- states first.
CREATE UNIQUE INDEX CONCURRENTLY uq_curtailment_event_one_non_terminal_per_org
    ON curtailment_event (org_id)
    WHERE state IN ('pending', 'active', 'restoring');
