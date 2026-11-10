-- requires-shared: 1073
LOCK TABLE organization IN EXCLUSIVE MODE;
DROP TRIGGER hashboard_reinstalled_pool_transition ON hashboard_installation;
DROP FUNCTION resume_reinstalled_board_pool_transition();
DROP TRIGGER hashboard_removed_pool_transitions ON hashboard_installation;
DROP FUNCTION retire_removed_board_pool_transitions();
DROP TRIGGER pool_lifecycle_absent_board ON pool_lifecycle_transition;
DROP FUNCTION retire_absent_board_pool_transition();
DROP INDEX idx_pool_lifecycle_removed_board;
-- Retired history remains retired; downgrade must not resurrect stale writes.
