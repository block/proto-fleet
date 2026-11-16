-- name: RegisterMiningGroupStep :exec
-- Commit with the encrypted journal before any controller dispatch.
INSERT INTO mining_group_step_dispatch(step_key,attempt_id) VALUES($1,$2);

-- name: MarkMiningGroupStepDispatch :execrows
-- Commit independently before I/O. A rollback of the binding transaction must
-- never make an attempted request look unsent. Missing history stays unknown.
INSERT INTO mining_group_step_dispatch(step_key,attempt_id,dispatch_started) VALUES($1,$2,TRUE)
ON CONFLICT(step_key) DO UPDATE SET dispatch_started=TRUE
WHERE mining_group_step_dispatch.attempt_id=EXCLUDED.attempt_id;

-- name: RestoreMiningGroupStepUnsent :execrows
-- Call only when the first marked attempt returned explicit proof that no
-- outbound request began. A caller must never erase an earlier uncertain send.
UPDATE mining_group_step_dispatch SET dispatch_started=FALSE
WHERE step_key=$1 AND attempt_id=$2 AND dispatch_started;

-- name: MiningGroupStepIsUnsent :one
-- The caller holds the workflow's journal and binding locks through retirement.
SELECT EXISTS(SELECT 1 FROM mining_group_step_dispatch WHERE step_key=$1 AND attempt_id=$2 AND NOT dispatch_started)::boolean;
