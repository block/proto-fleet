-- requires-shared: 1000
CREATE TABLE mining_group_step_dispatch (
 step_key UUID PRIMARY KEY,
 attempt_id UUID NOT NULL REFERENCES device_write_attempt(attempt_id),
 dispatch_started BOOLEAN NOT NULL DEFAULT FALSE,
 dispatch_token UUID NOT NULL DEFAULT gen_random_uuid()
);
CREATE INDEX idx_mining_group_step_dispatch_attempt ON mining_group_step_dispatch(attempt_id);
-- Existing saved requests have unknown delivery. Never backfill them as unsent.
