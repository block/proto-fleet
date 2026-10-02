-- Card summaries now derive from normalized inventory. Controller and plant
-- telemetry remain in their dedicated tables; no module history is removed.
DROP TABLE container_snapshot;
