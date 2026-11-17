-- requires-shared: 1000
-- Preserve every observation and the existing daily chunks/30-day retention.
-- Schedule compression rather than rewriting retained history in the migration.
ALTER TABLE hashboard_diagnostic_observation SET (
 timescaledb.compress,
 timescaledb.compress_segmentby = 'hashboard_id',
 timescaledb.compress_orderby = 'collected_at DESC'
);
SELECT add_compression_policy('hashboard_diagnostic_observation', INTERVAL '48 hours', schedule_interval => INTERVAL '1 hour');

ALTER TABLE container_module_diagnostic_observation SET (
 timescaledb.compress,
 timescaledb.compress_segmentby = 'module_id',
 timescaledb.compress_orderby = 'collected_at DESC'
);
SELECT add_compression_policy('container_module_diagnostic_observation', INTERVAL '48 hours', schedule_interval => INTERVAL '1 hour');
