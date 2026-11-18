-- Retained chunks must be decompressed before disabling compression. Rollback
-- needs enough free disk space and time to restore their uncompressed contents.
SELECT remove_compression_policy('hashboard_diagnostic_observation');
SELECT decompress_chunk(c, if_compressed => true) FROM show_chunks('hashboard_diagnostic_observation') c;
ALTER TABLE hashboard_diagnostic_observation SET (timescaledb.compress = false);

SELECT remove_compression_policy('container_module_diagnostic_observation');
SELECT decompress_chunk(c, if_compressed => true) FROM show_chunks('container_module_diagnostic_observation') c;
ALTER TABLE container_module_diagnostic_observation SET (timescaledb.compress = false);
