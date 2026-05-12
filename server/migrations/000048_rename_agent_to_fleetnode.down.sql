ALTER TABLE api_key DROP CONSTRAINT ck_api_key_subject;
UPDATE api_key SET subject_kind = 'agent' WHERE subject_kind = 'fleet_node';

ALTER TRIGGER update_fleet_node_updated_at ON fleet_node RENAME TO update_agent_updated_at;

ALTER TABLE fleet_node_session        RENAME CONSTRAINT fk_fleet_node_session_fleet_node           TO fk_agent_session_agent;
ALTER TABLE fleet_node_session        RENAME CONSTRAINT uq_fleet_node_session_fleet_node_id        TO uq_agent_session_agent_id;
ALTER TABLE fleet_node_auth_challenge RENAME CONSTRAINT fk_fleet_node_auth_challenge_fleet_node    TO fk_agent_auth_challenge_agent;
ALTER TABLE fleet_node_auth_challenge RENAME CONSTRAINT uq_fleet_node_auth_challenge_fleet_node_id TO uq_agent_auth_challenge_agent_id;
ALTER TABLE pending_enrollment        RENAME CONSTRAINT ck_pending_enrollment_fleet_node_states    TO ck_pending_enrollment_agent_states;
ALTER TABLE pending_enrollment        RENAME CONSTRAINT fk_pending_enrollment_fleet_node           TO fk_pending_enrollment_agent;
ALTER TABLE api_key                   RENAME CONSTRAINT fk_api_key_fleet_node                      TO fk_api_key_agent;
ALTER TABLE fleet_node_device         RENAME CONSTRAINT uq_fleet_node_device_device_id             TO uq_agent_device_device_id;
ALTER TABLE fleet_node_device         RENAME CONSTRAINT fk_fleet_node_device_assigned_by           TO fk_agent_device_assigned_by;
ALTER TABLE fleet_node_device         RENAME CONSTRAINT fk_fleet_node_device_device                TO fk_agent_device_device;
ALTER TABLE fleet_node_device         RENAME CONSTRAINT fk_fleet_node_device_fleet_node            TO fk_agent_device_agent;
ALTER TABLE fleet_node                RENAME CONSTRAINT ck_fleet_node_enrollment_status            TO ck_agent_enrollment_status;
ALTER TABLE fleet_node                RENAME CONSTRAINT uq_fleet_node_id_org_id                    TO uq_agent_id_org_id;
ALTER TABLE fleet_node                RENAME CONSTRAINT fk_fleet_node_org                          TO fk_agent_org;

ALTER INDEX idx_fleet_node_session_expires_at        RENAME TO idx_agent_session_expires_at;
ALTER INDEX idx_fleet_node_auth_challenge_expires_at RENAME TO idx_agent_auth_challenge_expires_at;
ALTER INDEX idx_pending_enrollment_fleet_node_id     RENAME TO idx_pending_enrollment_agent_id;
ALTER INDEX idx_api_key_fleet_node_id                RENAME TO idx_api_key_agent_id;
ALTER INDEX idx_fleet_node_device_org_id             RENAME TO idx_agent_device_org_id;
ALTER INDEX uq_fleet_node_org_name                   RENAME TO uq_agent_org_name;
ALTER INDEX uq_fleet_node_identity_pubkey            RENAME TO uq_agent_identity_pubkey;
ALTER INDEX idx_fleet_node_org_id                    RENAME TO idx_agent_org_id;
ALTER INDEX fleet_node_session_pkey                  RENAME TO agent_session_pkey;
ALTER INDEX fleet_node_auth_challenge_pkey           RENAME TO agent_auth_challenge_pkey;
ALTER INDEX fleet_node_device_pkey                   RENAME TO agent_device_pkey;
ALTER INDEX fleet_node_pkey                          RENAME TO agent_pkey;

ALTER TABLE fleet_node_session        RENAME COLUMN fleet_node_id TO agent_id;
ALTER TABLE fleet_node_auth_challenge RENAME COLUMN fleet_node_id TO agent_id;
ALTER TABLE api_key                   RENAME COLUMN fleet_node_id TO agent_id;
ALTER TABLE pending_enrollment        RENAME COLUMN fleet_node_id TO agent_id;
ALTER TABLE fleet_node_device         RENAME COLUMN fleet_node_id TO agent_id;

ALTER TABLE fleet_node_session        RENAME TO agent_session;
ALTER TABLE fleet_node_auth_challenge RENAME TO agent_auth_challenge;
ALTER TABLE fleet_node_device         RENAME TO agent_device;
ALTER TABLE fleet_node                RENAME TO agent;

ALTER TABLE api_key ADD CONSTRAINT ck_api_key_subject CHECK (
    (subject_kind = 'user'  AND user_id IS NOT NULL AND agent_id IS NULL) OR
    (subject_kind = 'agent' AND user_id IS NULL     AND agent_id IS NOT NULL)
);
