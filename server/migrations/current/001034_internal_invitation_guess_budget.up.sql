-- requires-shared: 1000
-- One durable budget per database, shared by every organization and API replica.
-- No caller-controlled keys, IP addresses, or invitation secrets are stored here.
CREATE TABLE hosted_invitation_guess_budget (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton)
);
INSERT INTO hosted_invitation_guess_budget (singleton) VALUES (TRUE);

-- At most 1,000 admitted checks in the rolling hour. Expired rows are pruned
-- on admission; idle deployments retain at most that many rows.
CREATE TABLE hosted_invitation_guess_attempt (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    attempted_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    exposure BIGINT NOT NULL CHECK (exposure > 0)
);

CREATE INDEX idx_hosted_account_invitation_active_expiry
    ON hosted_account_invitation (expires_at)
    WHERE revoked_at IS NULL AND accepted_at IS NULL;
