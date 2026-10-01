-- Restores the former cache schema. The previous collector repopulates it on
-- the next successful poll; discarded raw snapshots cannot be recovered.
CREATE TABLE container_snapshot (
    device_id BIGINT PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
    observed_at TIMESTAMPTZ NOT NULL,
    snapshot JSONB NOT NULL CHECK (jsonb_typeof(snapshot) = 'object')
);
