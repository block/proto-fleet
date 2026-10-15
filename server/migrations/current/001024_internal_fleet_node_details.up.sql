-- requires-shared: 1000

ALTER TABLE fleet_node_heartbeat
    ADD COLUMN ip_addresses text[] NOT NULL DEFAULT '{}',
    ADD COLUMN version text NOT NULL DEFAULT '';

ALTER TABLE pending_enrollment
    ADD COLUMN confirmed_by bigint CONSTRAINT fk_pending_enrollment_confirmed_by REFERENCES "user"(id);

CREATE INDEX idx_pending_enrollment_confirmed_node
    ON pending_enrollment (fleet_node_id, org_id, consumed_at DESC, id DESC)
    WHERE status = 'CONFIRMED';
