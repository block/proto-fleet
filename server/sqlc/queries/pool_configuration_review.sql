-- name: CreatePoolConfigurationReview :one
INSERT INTO pool_configuration_review(review_id, organization_id, configuration_id, actor_id, configuration_version, assignment_version, name, slots, settings_changed, targets, kind)
VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING *;

-- name: LockPoolConfigurationReview :one
-- Evaluate the deadline after acquiring the row lock, including any lock wait.
WITH locked AS MATERIALIZED (
 SELECT r.* FROM pool_configuration_review r
 JOIN pool_configuration c ON c.organization_id = r.organization_id AND c.configuration_id = r.configuration_id
 WHERE r.organization_id = sqlc.arg(organization_id) AND r.review_id = sqlc.arg(review_id) AND r.actor_id = sqlc.arg(actor_id)
  AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint
 FOR UPDATE OF r
)
SELECT locked.*, (locked.expires_at > clock_timestamp())::boolean AS valid FROM locked;

-- name: SavePoolConfigurationReviewResult :execrows
UPDATE pool_configuration_review SET saved_version = $1, saved_revision_id = $2, operation_id = $3
WHERE organization_id = $4 AND review_id = $5 AND saved_version IS NULL;
