-- requires-shared: 1000
CREATE TABLE hosted_invitation_rate_limit (
    client_hash TEXT PRIMARY KEY,
    window_started_at TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL CHECK (attempts > 0)
);

CREATE INDEX idx_hosted_invitation_rate_limit_window_started_at
    ON hosted_invitation_rate_limit (window_started_at);
