-- Rename agent vocabulary to fleet_node across schema. Empty tables in dev
-- and prod make every ALTER metadata-only.

ALTER TABLE agent                RENAME TO fleet_node;
ALTER TABLE agent_device         RENAME TO fleet_node_device;
ALTER TABLE agent_auth_challenge RENAME TO fleet_node_auth_challenge;
ALTER TABLE agent_session        RENAME TO fleet_node_session;

ALTER TABLE fleet_node_device         RENAME COLUMN agent_id TO fleet_node_id;
ALTER TABLE pending_enrollment        RENAME COLUMN agent_id TO fleet_node_id;
ALTER TABLE api_key                   RENAME COLUMN agent_id TO fleet_node_id;
ALTER TABLE fleet_node_auth_challenge RENAME COLUMN agent_id TO fleet_node_id;
ALTER TABLE fleet_node_session        RENAME COLUMN agent_id TO fleet_node_id;

ALTER INDEX agent_pkey                         RENAME TO fleet_node_pkey;
ALTER INDEX agent_device_pkey                  RENAME TO fleet_node_device_pkey;
ALTER INDEX agent_auth_challenge_pkey          RENAME TO fleet_node_auth_challenge_pkey;
ALTER INDEX agent_session_pkey                 RENAME TO fleet_node_session_pkey;
ALTER INDEX idx_agent_org_id                   RENAME TO idx_fleet_node_org_id;
ALTER INDEX uq_agent_identity_pubkey           RENAME TO uq_fleet_node_identity_pubkey;
ALTER INDEX uq_agent_org_name                  RENAME TO uq_fleet_node_org_name;
ALTER INDEX idx_agent_device_org_id            RENAME TO idx_fleet_node_device_org_id;
ALTER INDEX idx_api_key_agent_id               RENAME TO idx_api_key_fleet_node_id;
ALTER INDEX idx_pending_enrollment_agent_id    RENAME TO idx_pending_enrollment_fleet_node_id;
ALTER INDEX idx_agent_auth_challenge_expires_at RENAME TO idx_fleet_node_auth_challenge_expires_at;
ALTER INDEX idx_agent_session_expires_at       RENAME TO idx_fleet_node_session_expires_at;

ALTER TABLE fleet_node                RENAME CONSTRAINT fk_agent_org                  TO fk_fleet_node_org;
ALTER TABLE fleet_node                RENAME CONSTRAINT uq_agent_id_org_id            TO uq_fleet_node_id_org_id;
ALTER TABLE fleet_node                RENAME CONSTRAINT ck_agent_enrollment_status    TO ck_fleet_node_enrollment_status;
ALTER TABLE fleet_node_device         RENAME CONSTRAINT fk_agent_device_agent         TO fk_fleet_node_device_fleet_node;
ALTER TABLE fleet_node_device         RENAME CONSTRAINT fk_agent_device_device        TO fk_fleet_node_device_device;
ALTER TABLE fleet_node_device         RENAME CONSTRAINT fk_agent_device_assigned_by   TO fk_fleet_node_device_assigned_by;
ALTER TABLE fleet_node_device         RENAME CONSTRAINT uq_agent_device_device_id     TO uq_fleet_node_device_device_id;
ALTER TABLE api_key                   RENAME CONSTRAINT fk_api_key_agent              TO fk_api_key_fleet_node;
ALTER TABLE pending_enrollment        RENAME CONSTRAINT fk_pending_enrollment_agent   TO fk_pending_enrollment_fleet_node;
ALTER TABLE pending_enrollment        RENAME CONSTRAINT ck_pending_enrollment_agent_states TO ck_pending_enrollment_fleet_node_states;
ALTER TABLE fleet_node_auth_challenge RENAME CONSTRAINT uq_agent_auth_challenge_agent_id TO uq_fleet_node_auth_challenge_fleet_node_id;
ALTER TABLE fleet_node_auth_challenge RENAME CONSTRAINT fk_agent_auth_challenge_agent TO fk_fleet_node_auth_challenge_fleet_node;
ALTER TABLE fleet_node_session        RENAME CONSTRAINT uq_agent_session_agent_id     TO uq_fleet_node_session_fleet_node_id;
ALTER TABLE fleet_node_session        RENAME CONSTRAINT fk_agent_session_agent        TO fk_fleet_node_session_fleet_node;

ALTER TRIGGER update_agent_updated_at ON fleet_node RENAME TO update_fleet_node_updated_at;

-- subject_kind enum value change: 'agent' -> 'fleet_node'. CHECK body must
-- be rewritten because the literal value is embedded in the constraint.
ALTER TABLE api_key DROP CONSTRAINT ck_api_key_subject;
UPDATE api_key SET subject_kind = 'fleet_node' WHERE subject_kind = 'agent';
ALTER TABLE api_key ADD CONSTRAINT ck_api_key_subject CHECK (
    (subject_kind = 'user'       AND user_id IS NOT NULL AND fleet_node_id IS NULL) OR
    (subject_kind = 'fleet_node' AND user_id IS NULL     AND fleet_node_id IS NOT NULL)
);
