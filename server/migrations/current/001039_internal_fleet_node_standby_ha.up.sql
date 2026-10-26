-- requires-shared: 1000
-- A standby enrollment names the confirmed node it will back up.
ALTER TABLE pending_enrollment ADD COLUMN standby_for_node_id bigint;
ALTER TABLE pending_enrollment ADD CONSTRAINT fk_pending_enrollment_standby_for
    FOREIGN KEY (standby_for_node_id, org_id) REFERENCES fleet_node(id, org_id) ON DELETE CASCADE;

-- Two-node HA. The active node owns the pair's fleet_node_device rows;
-- failover moves them to the standby and swaps the roles. A node belongs to
-- at most one pair; enrollment enforces that across the two columns.
CREATE TABLE fleet_node_ha_pair (
    id bigserial PRIMARY KEY,
    org_id bigint NOT NULL,
    active_node_id bigint NOT NULL,
    standby_node_id bigint NOT NULL,
    -- True once the standby holds the active node's credential key.
    standby_ready boolean NOT NULL DEFAULT false,
    -- Sealed CredentialKeyResult persisted before dispatch, retained until ack.
    pending_credential_key bytea,
    status_reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    failed_over_at timestamptz,
    CONSTRAINT uq_fleet_node_ha_pair_active UNIQUE (active_node_id),
    CONSTRAINT uq_fleet_node_ha_pair_standby UNIQUE (standby_node_id),
    CONSTRAINT ck_fleet_node_ha_pair_distinct CHECK (active_node_id <> standby_node_id),
    CONSTRAINT fk_fleet_node_ha_pair_active FOREIGN KEY (active_node_id, org_id)
        REFERENCES fleet_node(id, org_id) ON DELETE CASCADE,
    CONSTRAINT fk_fleet_node_ha_pair_standby FOREIGN KEY (standby_node_id, org_id)
        REFERENCES fleet_node(id, org_id) ON DELETE CASCADE
);
