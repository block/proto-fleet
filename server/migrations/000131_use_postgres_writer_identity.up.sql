DROP VIEW connected_postgres_identity;

-- HA is not live yet, so discard the old etcd-derived singleton before
-- changing its cluster identity source.
TRUNCATE fleet_runtime_lease;
ALTER TABLE fleet_runtime_lease
    RENAME COLUMN dcs_cluster_id TO postgres_system_identifier;

CREATE VIEW connected_postgres_identity AS
SELECT
    (pg_control_system()).system_identifier::TEXT AS system_identifier,
    COALESCE(host(inet_server_addr()), '')::TEXT AS server_address,
    COALESCE(inet_server_port(), 0)::INTEGER AS server_port,
    pg_is_in_recovery() AS in_recovery,
    CASE
        WHEN pg_is_in_recovery() THEN 0
        ELSE (
            pg_split_walfile_name(pg_walfile_name(pg_current_wal_lsn()))
        ).timeline_id
    END::BIGINT AS timeline;
