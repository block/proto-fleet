-- requires-shared: 1000
-- Stop application and scheduled writers during this upgrade. Existing generic
-- summaries cannot prove allocation/source-time authorization; never seed this
-- history from them or project current allocations into older measurements.
LOCK TABLE device_metrics,miner_state_snapshots,container_module_metrics,
 container_module_diagnostic_observation,hosted_account_asset_interval IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE hosted_account_history_summary_epoch (
 singleton boolean CONSTRAINT pk_hosted_account_history_summary_epoch PRIMARY KEY DEFAULT true
  CONSTRAINT ck_hosted_account_history_summary_epoch_singleton CHECK(singleton),
 started_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO hosted_account_history_summary_epoch(singleton) VALUES(true);

CREATE FUNCTION hosted_history_metric_code(metric_name text) RETURNS smallint
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE metric_name WHEN 'hashrate' THEN 1 WHEN 'power' THEN 2
  WHEN 'temperature' THEN 3 WHEN 'state' THEN 4 END::smallint
$$;

-- Retain one independently authorized source per metric and raw observation.
-- The origin key includes the complete raw primary key, making retries and
-- corrections replacements rather than additional weight in an average.
CREATE TABLE hosted_account_history_sample (
 time timestamptz NOT NULL,
 interval_id bigint NOT NULL REFERENCES hosted_account_asset_interval(id),
 metric smallint NOT NULL CONSTRAINT ck_hosted_account_history_sample_metric CHECK(metric BETWEEN 1 AND 4),
 value double precision,
 complete boolean,
 source_oldest_at timestamptz NOT NULL,
 source_newest_at timestamptz NOT NULL,
 source_id smallint NOT NULL,
 origin_key text NOT NULL,
 origin_time timestamptz NOT NULL,
 origin_collected_at timestamptz NOT NULL,
 CONSTRAINT pk_hosted_account_history_sample PRIMARY KEY(time,source_id,origin_key,origin_time,origin_collected_at,metric,interval_id),
 CONSTRAINT ck_hosted_account_history_sample_source CHECK(source_newest_at>=source_oldest_at),
 CONSTRAINT ck_hosted_account_history_sample_value CHECK(value>'-Infinity'::float8 AND value<'Infinity'::float8)
);
SELECT create_hypertable('hosted_account_history_sample','time',chunk_time_interval=>INTERVAL '1 hour');
CREATE INDEX idx_hosted_account_history_sample_interval_time ON hosted_account_history_sample(interval_id,time DESC);
CREATE INDEX idx_hosted_account_asset_interval_capture_miner ON hosted_account_asset_interval(device_identifier,site_id,started_at) WHERE kind=5;
CREATE INDEX idx_hosted_account_asset_interval_capture_module ON hosted_account_asset_interval(asset_id,site_id,started_at) WHERE kind=6;
ALTER TABLE hosted_account_history_sample SET(timescaledb.compress,
 timescaledb.compress_segmentby='interval_id,metric',timescaledb.compress_orderby='time DESC');
SELECT add_compression_policy('hosted_account_history_sample',INTERVAL '6 hours',schedule_interval=>INTERVAL '1 hour');

-- One row per allocation/source period, with independent evidence per metric.
CREATE MATERIALIZED VIEW hosted_account_history_hourly
WITH(timescaledb.continuous,timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 hour',time) AS bucket,interval_id,
 sum(value) FILTER(WHERE metric=hosted_history_metric_code('hashrate')) AS hashrate_sum,
 count(value) FILTER(WHERE metric=hosted_history_metric_code('hashrate')) AS hashrate_count,
 count(value) FILTER(WHERE metric=hosted_history_metric_code('hashrate') AND value>0) AS hashrate_positive_count,
 bool_and(complete) FILTER(WHERE metric=hosted_history_metric_code('hashrate')) AS hashrate_complete,
 min(source_oldest_at) FILTER(WHERE metric=hosted_history_metric_code('hashrate')) AS hashrate_source_oldest_at,
 max(source_newest_at) FILTER(WHERE metric=hosted_history_metric_code('hashrate')) AS hashrate_source_newest_at,
 sum(value) FILTER(WHERE metric=hosted_history_metric_code('power')) AS power_sum,
 count(value) FILTER(WHERE metric=hosted_history_metric_code('power')) AS power_count,
 bool_and(complete) FILTER(WHERE metric=hosted_history_metric_code('power')) AS power_complete,
 min(source_oldest_at) FILTER(WHERE metric=hosted_history_metric_code('power')) AS power_source_oldest_at,
 max(source_newest_at) FILTER(WHERE metric=hosted_history_metric_code('power')) AS power_source_newest_at,
 sum(value) FILTER(WHERE metric=hosted_history_metric_code('temperature')) AS temperature_sum,
 count(value) FILTER(WHERE metric=hosted_history_metric_code('temperature')) AS temperature_count,
 last(value,time) FILTER(WHERE metric=hosted_history_metric_code('temperature') AND value IS NOT NULL) AS temperature_latest_value,
 max(time) FILTER(WHERE metric=hosted_history_metric_code('temperature') AND value IS NOT NULL) AS temperature_latest_at,
 min(source_oldest_at) FILTER(WHERE metric=hosted_history_metric_code('temperature')) AS temperature_source_oldest_at,
 max(source_newest_at) FILTER(WHERE metric=hosted_history_metric_code('temperature')) AS temperature_source_newest_at,
 count(value) FILTER(WHERE metric=hosted_history_metric_code('state')) AS state_count,
 last(value,time) FILTER(WHERE metric=hosted_history_metric_code('state') AND value IS NOT NULL) AS state_latest_value,
 max(time) FILTER(WHERE metric=hosted_history_metric_code('state') AND value IS NOT NULL) AS state_latest_at,
 min(source_oldest_at) FILTER(WHERE metric=hosted_history_metric_code('state')) AS state_source_oldest_at,
 max(source_newest_at) FILTER(WHERE metric=hosted_history_metric_code('state')) AS state_source_newest_at
FROM hosted_account_history_sample GROUP BY bucket,interval_id WITH NO DATA;
CREATE INDEX idx_hosted_account_history_hourly_interval_bucket ON hosted_account_history_hourly(interval_id,bucket DESC);

-- Roll up hourly rows without collapsing metric-specific source windows.
CREATE MATERIALIZED VIEW hosted_account_history_daily
WITH(timescaledb.continuous,timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '24 hours',bucket) AS bucket,interval_id,
 sum(hashrate_sum) AS hashrate_sum,
 sum(hashrate_count)::bigint AS hashrate_count,
 sum(hashrate_positive_count)::bigint AS hashrate_positive_count,
 bool_and(hashrate_complete) AS hashrate_complete,
 min(hashrate_source_oldest_at) AS hashrate_source_oldest_at,
 max(hashrate_source_newest_at) AS hashrate_source_newest_at,
 sum(power_sum) AS power_sum,
 sum(power_count)::bigint AS power_count,
 bool_and(power_complete) AS power_complete,
 min(power_source_oldest_at) AS power_source_oldest_at,
 max(power_source_newest_at) AS power_source_newest_at,
 sum(temperature_sum) AS temperature_sum,
 sum(temperature_count)::bigint AS temperature_count,
 last(temperature_latest_value,temperature_latest_at) FILTER(WHERE temperature_latest_at IS NOT NULL) AS temperature_latest_value,
 max(temperature_latest_at) AS temperature_latest_at,
 min(temperature_source_oldest_at) AS temperature_source_oldest_at,
 max(temperature_source_newest_at) AS temperature_source_newest_at,
 sum(state_count)::bigint AS state_count,
 last(state_latest_value,state_latest_at) FILTER(WHERE state_latest_at IS NOT NULL) AS state_latest_value,
 max(state_latest_at) AS state_latest_at,
 min(state_source_oldest_at) AS state_source_oldest_at,
 max(state_source_newest_at) AS state_source_newest_at
FROM hosted_account_history_hourly GROUP BY 1,interval_id WITH NO DATA;
CREATE INDEX idx_hosted_account_history_daily_interval_bucket ON hosted_account_history_daily(interval_id,bucket DESC);

-- The caller scopes intervals to the requested account. Share physical-site
-- authority before expanding observations; source-time checks remain per metric.
CREATE FUNCTION hosted_history_authorized_intervals(requested jsonb,site_filter bigint,
 policy_restricted boolean,policy_sites bigint[],host_authority jsonb)
RETURNS TABLE(interval_id bigint,kind integer,asset_id bigint,device_identifier text,
 site_id bigint,started_at timestamptz,ended_at timestamptz)
LANGUAGE sql STABLE PARALLEL SAFE AS $$
 SELECT i.* FROM jsonb_to_recordset(requested) AS i(interval_id bigint,kind integer,
  asset_id bigint,device_identifier text,site_id bigint,started_at timestamptz,ended_at timestamptz)
 WHERE (site_filter=0 OR i.site_id=site_filter)
  AND (NOT policy_restricted OR i.site_id=ANY(policy_sites))
  AND NOT EXISTS (
   SELECT 1 FROM jsonb_to_recordset(host_authority) AS a(org_wide boolean,sites bigint[])
   WHERE NOT (CASE WHEN a.org_wide THEN i.site_id=0 OR NOT(i.site_id=ANY(a.sites)) ELSE i.site_id=ANY(a.sites) END)
  )
$$;

-- Keep Timescale's catalog behind a typed read interface. Uninitialized
-- watermarks are -infinity: charge the full available request in that case.
CREATE FUNCTION hosted_account_history_watermarks(available_from timestamptz)
RETURNS TABLE(hourly timestamptz,daily timestamptz)
LANGUAGE sql STABLE AS $$
 SELECT GREATEST(available_from,COALESCE(
  _timescaledb_functions.to_timestamp(_timescaledb_functions.cagg_watermark(h.mat_hypertable_id)),available_from)),
 GREATEST(available_from,COALESCE(
  _timescaledb_functions.to_timestamp(_timescaledb_functions.cagg_watermark(d.mat_hypertable_id)),available_from))
 FROM _timescaledb_catalog.continuous_agg h
 CROSS JOIN _timescaledb_catalog.continuous_agg d
 WHERE h.user_view_schema='public' AND h.user_view_name='hosted_account_history_hourly'
  AND d.user_view_schema='public' AND d.user_view_name='hosted_account_history_daily'
$$;

-- Standard Timescale policies maintain recent summaries. No historical bootstrap
-- or catch-up state is needed for this prospective stream.
DO $$ DECLARE
 raw_retention CONSTANT interval := INTERVAL '10 days';
 refresh_window CONSTANT interval := raw_retention-INTERVAL '1 day';
 hourly_refresh_cadence CONSTANT interval := INTERVAL '30 minutes';
 daily_refresh_cadence CONSTANT interval := INTERVAL '6 hours';
 hourly_retention CONSTANT interval := INTERVAL '3 months';
 daily_retention CONSTANT interval := INTERVAL '3 years';
BEGIN
 PERFORM add_continuous_aggregate_policy('hosted_account_history_hourly',
  start_offset=>refresh_window,end_offset=>INTERVAL '1 hour',schedule_interval=>hourly_refresh_cadence);
 PERFORM add_continuous_aggregate_policy('hosted_account_history_daily',
  start_offset=>refresh_window,end_offset=>INTERVAL '1 day',schedule_interval=>daily_refresh_cadence);
 PERFORM add_retention_policy('hosted_account_history_sample',raw_retention);
 PERFORM add_retention_policy('hosted_account_history_hourly',hourly_retention);
 PERFORM add_retention_policy('hosted_account_history_daily',daily_retention);
END $$;

-- Raw and retained module charts must classify a state identically.
CREATE FUNCTION hosted_module_uptime_state(state text) RETURNS smallint
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE WHEN state='active' THEN 3
  WHEN state IN ('error','needs_mining_pool','updating','reboot_required') THEN 2
  WHEN state='offline' THEN 0
  WHEN state IN ('inactive','maintenance') THEN 1
  ELSE 4 END::smallint
$$;

-- Only malformed coverage conversions become unavailable evidence. Errors from
-- capture writes still propagate; this does not hide database/schema failures.
CREATE FUNCTION hosted_history_metric_coverage(coverage jsonb)
RETURNS TABLE(oldest_at timestamptz,newest_at timestamptz,complete boolean)
LANGUAGE sql STABLE AS $$
 WITH validity AS (
  SELECT COALESCE(pg_input_is_valid(coverage->>'oldest_at','timestamptz'),true)
   AND COALESCE(pg_input_is_valid(coverage->>'newest_at','timestamptz'),true)
   AND COALESCE(pg_input_is_valid(coverage->>'topology_complete','boolean'),true)
   AND COALESCE(pg_input_is_valid(coverage->>'expected','integer'),true)
   AND COALESCE(pg_input_is_valid(coverage->>'reporting','integer'),true) AS valid
 )
 SELECT CASE WHEN valid THEN (coverage->>'oldest_at')::timestamptz END,
  CASE WHEN valid THEN (coverage->>'newest_at')::timestamptz END,
  CASE WHEN valid THEN COALESCE((coverage->>'topology_complete')::boolean
   AND (coverage->>'expected')::integer>0
   AND (coverage->>'reporting')::integer=(coverage->>'expected')::integer,false) ELSE false END
 FROM validity
$$;

CREATE FUNCTION record_hosted_account_history_sample() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 miner_metric_source CONSTANT smallint := 1;
 miner_state_source CONSTANT smallint := 2;
 module_metric_source CONSTANT smallint := 3;
 module_state_source CONSTANT smallint := 4;
 hashrate_metric CONSTANT smallint := hosted_history_metric_code('hashrate');
 power_metric CONSTANT smallint := hosted_history_metric_code('power');
 temperature_metric CONSTANT smallint := hosted_history_metric_code('temperature');
 state_metric CONSTANT smallint := hosted_history_metric_code('state');
 miner_asset_kind CONSTANT integer := 5;
 module_asset_kind CONSTANT integer := 6;
 source_freshness CONSTANT interval := INTERVAL '2 minutes';
 source_clock_skew CONSTANT interval := INTERVAL '5 seconds';
 row_data jsonb;
 previous_data jsonb;
 source_number smallint;
 asset_kind integer;
 asset bigint;
 identifier text;
 site bigint;
 organization bigint;
 origin_at timestamptz;
 collected_at timestamptz;
 original_key text;
 previous_times timestamptz[];
 epoch timestamptz;
BEGIN
 -- Hypertable row triggers execute on physical chunks, whose TG_TABLE_NAME is
 -- not the logical source name. Keep source identity in the trigger argument.
 source_number := CASE TG_ARGV[0]
  WHEN 'miner_metrics' THEN miner_metric_source WHEN 'miner_state' THEN miner_state_source
  WHEN 'module_metrics' THEN module_metric_source WHEN 'module_state' THEN module_state_source END;
 -- Most estate telemetry need not enter hosted history. Skip unallocated
 -- observations before normalizing their individual metrics.
 IF TG_OP='INSERT' THEN
  IF source_number IN (miner_metric_source,miner_state_source) THEN
   -- Literal kinds let generic plans prove the capture partial-index predicates.
   IF NOT EXISTS(SELECT 1 FROM hosted_account_asset_interval i WHERE i.kind=5
    AND i.device_identifier=NEW.device_identifier AND COALESCE(i.site_id,0)=COALESCE(NEW.site_id,0)
    AND i.started_at<=NEW.time AND (i.ended_at IS NULL OR NEW.time<i.ended_at)) THEN RETURN NEW; END IF;
  ELSE
   IF NOT EXISTS(SELECT 1 FROM hosted_account_asset_interval i WHERE i.kind=6
    AND i.asset_id=NEW.module_id AND COALESCE(i.site_id,0)=COALESCE(NEW.site_id,0)
    AND i.started_at<=NEW.collected_at+source_clock_skew
    AND (i.ended_at IS NULL OR NEW.collected_at<i.ended_at+source_freshness)) THEN RETURN NEW; END IF;
  END IF;
 END IF;
 IF TG_OP IN ('UPDATE','DELETE') THEN
  previous_data := to_jsonb(OLD);
  original_key := COALESCE(previous_data->>'device_identifier',previous_data->>'module_id');
  origin_at := COALESCE((previous_data->>'time')::timestamptz,(previous_data->>'collected_at')::timestamptz);
  collected_at := COALESCE((previous_data->>'collected_at')::timestamptz,origin_at);
  previous_times := ARRAY[origin_at,collected_at,(previous_data->>'state_observed_at')::timestamptz,
   (SELECT newest_at FROM hosted_history_metric_coverage(previous_data#>'{coverage,hashrate}')),
   (SELECT newest_at FROM hosted_history_metric_coverage(previous_data#>'{coverage,power}')),
   (SELECT newest_at FROM hosted_history_metric_coverage(previous_data#>'{coverage,temperature}'))];
  DELETE FROM hosted_account_history_sample
  WHERE time=ANY(previous_times) AND source_id=source_number
   AND origin_key=original_key AND origin_time=origin_at AND origin_collected_at=collected_at;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 row_data := to_jsonb(NEW);
 asset_kind := CASE WHEN source_number IN (miner_metric_source,miner_state_source) THEN miner_asset_kind ELSE module_asset_kind END;
 asset := (row_data->>'module_id')::bigint;
 identifier := row_data->>'device_identifier';
 site := COALESCE((row_data->>'site_id')::bigint,0);
 organization := (row_data->>'org_id')::bigint;
 original_key := COALESCE(identifier,asset::text);
 origin_at := COALESCE((row_data->>'time')::timestamptz,(row_data->>'collected_at')::timestamptz);
 collected_at := COALESCE((row_data->>'collected_at')::timestamptz,origin_at);
 SELECT started_at INTO STRICT epoch FROM hosted_account_history_summary_epoch WHERE singleton;

 INSERT INTO hosted_account_history_sample(time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,
  source_id,origin_key,origin_time,origin_collected_at)
 SELECT samples.newest_at,i.id,samples.metric,
  CASE WHEN samples.metric IN (hashrate_metric,power_metric) THEN CASE WHEN samples.value>=0 AND samples.value<'Infinity'::float8 THEN samples.value END
   ELSE CASE WHEN samples.value>'-Infinity'::float8 AND samples.value<'Infinity'::float8 THEN samples.value END END,
  samples.complete,samples.oldest_at,samples.newest_at,source_number,original_key,origin_at,collected_at
 FROM (
  -- Miner samples have one observation timestamp; paired validity is shared
  -- by hash and power so invalid/missing inputs cannot manufacture efficiency.
  SELECT metric,value,origin_at AS oldest_at,origin_at AS newest_at,
   CASE WHEN metric IN (hashrate_metric,power_metric) THEN COALESCE(
    (row_data->>'hash_rate_hs')::float8>=0 AND (row_data->>'hash_rate_hs')::float8<'Infinity'::float8
    AND (row_data->>'power_w')::float8>=0 AND (row_data->>'power_w')::float8<'Infinity'::float8,false) END AS complete
  FROM (VALUES(hashrate_metric,(row_data->>'hash_rate_hs')::float8),
   (power_metric,(row_data->>'power_w')::float8),(temperature_metric,(row_data->>'temp_c')::float8)) miner(metric,value)
  WHERE source_number=miner_metric_source
  UNION ALL
  SELECT state_metric,CASE WHEN source_number=miner_state_source THEN (row_data->>'state')::float8 END,
   origin_at,origin_at,NULL::boolean WHERE source_number=miner_state_source
  UNION ALL
  -- Preserve each module contributor window, including missing-source markers
  -- for efficiency coverage. A different metric's timestamp grants no access.
  SELECT metric,CASE WHEN has_source THEN value END,
   CASE WHEN has_source THEN oldest_at ELSE collected_at END,
   CASE WHEN has_source THEN newest_at ELSE collected_at END,
   CASE WHEN metric IN (hashrate_metric,power_metric) THEN has_source AND source.complete
    AND COALESCE(value>=0 AND value<'Infinity'::float8,false) END
  FROM (VALUES
   (hashrate_metric,'hashrate',(row_data->>'hashrate_hs')::float8),
   (power_metric,'power',(row_data->>'power_w')::float8),
   (temperature_metric,'temperature',(row_data->>'temperature_c')::float8)
  ) module(metric,name,value)
  CROSS JOIN LATERAL hosted_history_metric_coverage(row_data->'coverage'->name) source
  CROSS JOIN LATERAL(SELECT COALESCE(oldest_at>'0001-01-01T00:00:00Z'::timestamptz
   AND isfinite(oldest_at) AND isfinite(newest_at) AND newest_at>=oldest_at,false) AS has_source) validity
  WHERE source_number=module_metric_source AND (has_source OR metric IN (hashrate_metric,power_metric))
  UNION ALL
  SELECT state_metric,hosted_module_uptime_state(row_data->>'state')::float8,
   (row_data->>'state_observed_at')::timestamptz,(row_data->>'state_observed_at')::timestamptz,NULL::boolean
  WHERE source_number=module_state_source
   AND (row_data->>'state_observed_at')::timestamptz>collected_at-source_freshness
   AND (row_data->>'state_observed_at')::timestamptz<=collected_at+source_clock_skew
   AND ((row_data->>'attribution_since') IS NULL
    OR (row_data->>'state_observed_at')::timestamptz>=(row_data->>'attribution_since')::timestamptz)
 ) samples
 JOIN hosted_account_asset_interval i ON i.kind=asset_kind
  AND (CASE WHEN asset_kind=miner_asset_kind THEN i.device_identifier=identifier ELSE i.asset_id=asset END)
  AND COALESCE(i.site_id,0)=site AND (organization IS NULL OR i.organization_id=organization)
  AND samples.oldest_at>=i.started_at AND (i.ended_at IS NULL OR samples.newest_at<i.ended_at)
  AND (asset_kind=miner_asset_kind OR collected_at>=i.started_at-source_clock_skew
   AND (i.ended_at IS NULL OR collected_at<i.ended_at+source_freshness))
 WHERE samples.oldest_at>=epoch
 ON CONFLICT(time,source_id,origin_key,origin_time,origin_collected_at,metric,interval_id) DO UPDATE
 SET value=EXCLUDED.value,complete=EXCLUDED.complete,source_oldest_at=EXCLUDED.source_oldest_at,source_newest_at=EXCLUDED.source_newest_at;
 RETURN NEW;
END;
$$;
CREATE TRIGGER record_hosted_account_history AFTER INSERT OR UPDATE OR DELETE ON device_metrics
 FOR EACH ROW EXECUTE FUNCTION record_hosted_account_history_sample('miner_metrics');
CREATE TRIGGER record_hosted_account_history AFTER INSERT OR UPDATE OR DELETE ON miner_state_snapshots
 FOR EACH ROW EXECUTE FUNCTION record_hosted_account_history_sample('miner_state');
CREATE TRIGGER record_hosted_account_history AFTER INSERT OR UPDATE OR DELETE ON container_module_metrics
 FOR EACH ROW EXECUTE FUNCTION record_hosted_account_history_sample('module_metrics');
CREATE TRIGGER record_hosted_account_history AFTER INSERT OR UPDATE OR DELETE ON container_module_diagnostic_observation
 FOR EACH ROW EXECUTE FUNCTION record_hosted_account_history_sample('module_state');
