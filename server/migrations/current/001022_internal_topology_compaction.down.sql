-- Hold the evidence check and DDL under the same migration transaction.
LOCK TABLE container_topology_observation IN ACCESS EXCLUSIVE MODE;
-- Dropping confirmation spans after deleting their receipts loses evidence.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM container_topology_observation WHERE last_confirmed_at>collected_at) THEN
  RAISE EXCEPTION 'topology has been compacted; disable cleanup instead of dropping confirmation evidence';
 END IF;
END $$;
SELECT delete_job(job_id) FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job';
DROP PROCEDURE compact_container_topology_job(integer,jsonb);
DROP FUNCTION compact_container_topology(bigint,timestamptz,integer);
DROP FUNCTION container_topology_candidates(bigint,timestamptz,integer);
DROP FUNCTION container_topology_references(bigint[]);
DROP TRIGGER register_container_topology_compaction ON container_topology_observation;
DROP FUNCTION register_container_topology_compaction();
DROP TABLE container_topology_compaction_pending;
DROP TABLE container_topology_compaction_state;
DROP INDEX idx_device_metrics_topology_observation;
DROP INDEX idx_container_tank_metrics_topology_observation;
DROP INDEX idx_container_tank_latest_metrics_topology_observation;
DROP INDEX idx_container_latest_metrics_topology_observation;
ALTER TABLE container_topology_observation DROP COLUMN last_confirmed_at;
