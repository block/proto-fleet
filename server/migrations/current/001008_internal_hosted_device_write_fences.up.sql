-- requires-shared: 1000

-- Reuse the composite device identity constraint established by migration 39.
CREATE TABLE device_write_fence (
 device_id BIGINT PRIMARY KEY,
 organization_id BIGINT NOT NULL,
 epoch BIGINT NOT NULL DEFAULT 0 CHECK (epoch >= 0),
 state TEXT NOT NULL DEFAULT 'host' CHECK (state IN ('host','reserved','active','quarantined')),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
 FOREIGN KEY (device_id, organization_id) REFERENCES device(id,org_id),
 CONSTRAINT uq_device_write_fence_device_org UNIQUE (device_id, organization_id)
);

ALTER TABLE queue_message ADD COLUMN write_epoch BIGINT NOT NULL DEFAULT 0 CHECK (write_epoch >= 0);

-- Attempt lifetime is independent of queue status and has no expiry. Reaping a
-- message cannot establish that a previously dispatched firmware call drained.
CREATE TABLE device_write_attempt (
 attempt_id UUID PRIMARY KEY,
 device_id BIGINT NOT NULL,
 organization_id BIGINT NOT NULL,
 epoch BIGINT NOT NULL CHECK (epoch >= 0),
 queue_message_id BIGINT NOT NULL REFERENCES queue_message(id),
 owner_instance_id UUID NOT NULL,
 operation_kind TEXT NOT NULL DEFAULT 'mutation' CHECK (operation_kind IN ('mutation','observation')),
 identity_snapshot JSONB NOT NULL,
 status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running','complete','unknown','resolved')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
 finished_at TIMESTAMPTZ,
 FOREIGN KEY (device_id, organization_id) REFERENCES device_write_fence(device_id,organization_id),
 CHECK ((status='running') = (finished_at IS NULL))
);
CREATE INDEX idx_device_write_attempt_outstanding ON device_write_attempt(device_id) WHERE status NOT IN ('complete','resolved');
CREATE INDEX idx_device_write_attempt_queue ON device_write_attempt(queue_message_id);

-- Recovery preserves the original attempt and distinguishes operator resolution
-- from successful firmware completion. Database credentials authorize this
-- offline operational boundary; evidence is explicitly operator-attested.
CREATE TABLE device_write_recovery (
 recovery_id UUID PRIMARY KEY,
 attempt_id UUID NOT NULL REFERENCES device_write_attempt(attempt_id),
 CONSTRAINT uq_device_write_recovery_attempt UNIQUE (attempt_id),
 fence_epoch BIGINT NOT NULL CHECK (fence_epoch >= 0),
 prior_status TEXT NOT NULL CHECK (prior_status IN ('running','unknown')),
 operator_name TEXT NOT NULL CHECK (length(operator_name) > 0),
 database_actor TEXT NOT NULL DEFAULT session_user,
 evidence JSONB NOT NULL,
 evidence_sha256 TEXT NOT NULL CHECK (length(evidence_sha256) = 64),
 created_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp()
);
