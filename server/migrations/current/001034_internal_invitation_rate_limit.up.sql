-- requires-shared: 1000
CREATE TABLE hosted_invitation_rate_limit (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton)
);
INSERT INTO hosted_invitation_rate_limit (singleton) VALUES (TRUE);

CREATE TABLE hosted_invitation_rate_limit_attempt (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    attempted_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    exposure BIGINT NOT NULL CHECK (exposure > 0)
);

CREATE INDEX idx_hosted_account_invitation_active_expiry
    ON hosted_account_invitation (expires_at)
    WHERE revoked_at IS NULL AND accepted_at IS NULL;
