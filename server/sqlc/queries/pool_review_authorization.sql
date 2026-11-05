-- name: AuthorizePoolConfigurationReviews :execrows
INSERT INTO pool_configuration_review_authorization(review_id, session_hash, credential_fingerprint)
SELECT r.review_id, sqlc.arg(session_hash), sqlc.arg(credential_fingerprint)
FROM pool_configuration_review r
WHERE r.organization_id = sqlc.arg(organization_id) AND r.actor_id = sqlc.arg(actor_id)
 AND r.review_id = ANY(sqlc.arg(review_ids)::text[])
ON CONFLICT (review_id, session_hash) DO UPDATE SET
 credential_fingerprint = EXCLUDED.credential_fingerprint, authorized_at = clock_timestamp();

-- name: HasPoolConfigurationReviewAuthorization :one
-- Confirmation holds the review lock and checks replay/expiry first. Use that
-- single deadline decision so expiry cannot become a missing-approval error.
SELECT EXISTS (
 SELECT 1 FROM pool_configuration_review_authorization a
 JOIN pool_configuration_review r ON r.review_id = a.review_id
 WHERE r.organization_id = sqlc.arg(organization_id) AND r.actor_id = sqlc.arg(actor_id)
  AND r.review_id = sqlc.arg(review_id)
  AND a.session_hash = sqlc.arg(session_hash) AND a.credential_fingerprint = sqlc.arg(credential_fingerprint)
);
