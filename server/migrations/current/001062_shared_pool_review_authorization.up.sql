-- Approval is limited to immutable reviews, the initiating session, and the
-- verified credential version. Review expiry and live authority still apply.
CREATE TABLE pool_configuration_review_authorization (
    review_id VARCHAR(36) NOT NULL REFERENCES pool_configuration_review(review_id) ON DELETE CASCADE,
    session_hash TEXT NOT NULL,
    credential_fingerprint TEXT NOT NULL,
    authorized_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (review_id, session_hash)
);
