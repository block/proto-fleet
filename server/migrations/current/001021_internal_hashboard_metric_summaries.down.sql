-- Serialize the evidence check with ingestion and summary refresh.
LOCK TABLE hashboard_metrics IN ACCESS EXCLUSIVE MODE;
-- Summaries may be the only remaining board evidence. Preserve them and fix
-- forward; an application rollback does not require dropping this schema.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM hashboard_metrics_hourly) OR EXISTS (SELECT 1 FROM hashboard_metrics_daily) THEN
  RAISE EXCEPTION 'hashboard summaries contain retained evidence; disable jobs or roll back the application instead';
 END IF;
END $$;
SELECT delete_job(job_id) FROM timescaledb_information.jobs WHERE proc_name='initialize_hashboard_metric_summaries';
DROP PROCEDURE initialize_hashboard_metric_summaries(integer,jsonb);
DROP VIEW hashboard_metric_summaries;
DROP MATERIALIZED VIEW hashboard_metrics_daily;
DROP MATERIALIZED VIEW hashboard_metrics_hourly;
ALTER TABLE hashboard_metrics DROP COLUMN topology_version_id;
SELECT remove_retention_policy('hashboard_metrics',if_exists=>true);
SELECT add_retention_policy('hashboard_metrics',INTERVAL '30 days');
