-- Ownership and audit history remain valid when automatic detection is disabled.
DROP FUNCTION reconcile_hosted_hashboard_replacements(bigint,bigint,timestamptz);
DROP INDEX idx_hashboard_installation_slot_history;
