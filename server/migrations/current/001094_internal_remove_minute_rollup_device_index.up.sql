-- requires-shared: 1000
-- Both minute-rollup readers filter by org_id. Keep their org/device/bucket
-- index and the bucket-only and org/bucket indexes used by fleet-wide reads.
-- Dropping the parent index also removes its chunk indexes and stops Timescale
-- from copying it to future chunks. The migration runner owns the transaction.
DO $$
DECLARE
    materialization_schema text;
    materialization_table text;
BEGIN
    SELECT materialization_hypertable_schema, materialization_hypertable_name
    INTO STRICT materialization_schema, materialization_table
    FROM timescaledb_information.continuous_aggregates
    WHERE view_schema = 'public'
      AND view_name = 'miner_state_snapshot_device_1m';

    -- Resolve the generated name rather than assuming a materialization ID.
    EXECUTE format('DROP INDEX %I.%I', materialization_schema,
                   materialization_table || '_device_identifier_bucket_idx');
END
$$;
