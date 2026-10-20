-- requires-shared: 1000
-- Keep collection bounds usable on compressed module history without assuming
-- that the partition timestamp and independent contributor windows agree.
ALTER TABLE container_module_metrics SET (
 timescaledb.compress_orderby = 'time DESC'
);
-- Changing the hypertable setting affects new chunks. Apply it to retained
-- compressed chunks too. Rehearse recompression/storage in the upgrade window.
DO $$
DECLARE chunk regclass;
BEGIN
 FOR chunk IN
  SELECT format('%I.%I',chunk_schema,chunk_name)::regclass
  FROM timescaledb_information.chunks
  WHERE hypertable_schema='public' AND hypertable_name='container_module_metrics' AND is_compressed
 LOOP
  PERFORM compress_chunk(chunk,if_not_compressed=>true,recompress=>true);
 END LOOP;
END;
$$;
