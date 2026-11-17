-- An explicit downgrade rebuilds every chunk index: allow a maintenance window
-- and enough free storage/WAL capacity. Application rollback does not run this.
DO $$
DECLARE
    materialization_table text;
BEGIN
    SELECT materialization_hypertable_name
    INTO STRICT materialization_table
    FROM timescaledb_information.continuous_aggregates
    WHERE view_schema = 'public'
      AND view_name = 'miner_state_snapshot_device_1m';

    -- Restore the original Timescale-generated name through the aggregate API.
    EXECUTE format(
        'CREATE INDEX %I ON public.miner_state_snapshot_device_1m (device_identifier, bucket DESC)',
        materialization_table || '_device_identifier_bucket_idx');
END
$$;
