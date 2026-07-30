CREATE TABLE fleet_runtime_lease (
    lease_name TEXT PRIMARY KEY
        CONSTRAINT fleet_runtime_lease_singleton CHECK (lease_name = 'fleet-active'),
    dcs_cluster_id TEXT NOT NULL,
    highest_writer_generation BIGINT NOT NULL
        CONSTRAINT fleet_runtime_lease_writer_generation_positive
        CHECK (highest_writer_generation > 0),
    lease_epoch BIGINT NOT NULL
        CONSTRAINT fleet_runtime_lease_epoch_positive
        CHECK (lease_epoch > 0),
    holder_id UUID NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE VIEW connected_postgres_identity AS
SELECT
    COALESCE(host(inet_server_addr()), '')::TEXT AS server_address,
    COALESCE(inet_server_port(), 0)::INTEGER AS server_port,
    pg_is_in_recovery() AS in_recovery,
    (pg_control_checkpoint()).timeline_id::BIGINT AS timeline;
