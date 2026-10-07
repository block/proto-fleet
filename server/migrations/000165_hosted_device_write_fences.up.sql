ALTER TABLE device ADD CONSTRAINT uq_device_id_org UNIQUE USING INDEX uq_device_id_org_idx;

CREATE TABLE device_write_fence (
 device_id BIGINT PRIMARY KEY,
 organization_id BIGINT NOT NULL,
 epoch BIGINT NOT NULL DEFAULT 0 CHECK (epoch >= 0),
 state TEXT NOT NULL DEFAULT 'host' CHECK (state IN ('host','reserved','active','quarantined')),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
 FOREIGN KEY (device_id, organization_id) REFERENCES device(id,org_id),
 UNIQUE (device_id, organization_id)
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
 status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running','complete','unknown')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
 finished_at TIMESTAMPTZ,
 FOREIGN KEY (device_id, organization_id) REFERENCES device_write_fence(device_id,organization_id),
 CHECK ((status='running') = (finished_at IS NULL))
);
CREATE INDEX device_write_attempt_outstanding ON device_write_attempt(device_id) WHERE status <> 'complete';
CREATE INDEX device_write_attempt_queue ON device_write_attempt(queue_message_id);
