DROP VIEW connected_postgres_identity;

TRUNCATE fleet_runtime_lease;
ALTER TABLE fleet_runtime_lease
    RENAME COLUMN postgres_system_identifier TO dcs_cluster_id;

CREATE VIEW connected_postgres_identity AS
SELECT
    COALESCE(host(inet_server_addr()), '')::TEXT AS server_address,
    COALESCE(inet_server_port(), 0)::INTEGER AS server_port,
    pg_is_in_recovery() AS in_recovery,
    (pg_control_checkpoint()).timeline_id::BIGINT AS timeline;
