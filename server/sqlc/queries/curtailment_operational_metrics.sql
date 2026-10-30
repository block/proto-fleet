-- name: GetCurtailmentPendingWorkAges :one
-- Global operational gauges; no pending work emits zero to clear old values.
SELECT
  COALESCE((SELECT GREATEST(0, EXTRACT(EPOCH FROM NOW() - MIN(created_at)))::float8
            FROM curtailment_event WHERE state = 'pending'), 0)::float8 AS pending_event_age_seconds,
  COALESCE((SELECT GREATEST(0, EXTRACT(EPOCH FROM NOW() - MIN(st.pending_received_at)))::float8
            FROM curtailment_mqtt_source_state st
            JOIN curtailment_mqtt_source_config src ON src.id = st.source_config_id
            WHERE src.enabled AND st.pending_direction IS NOT NULL), 0)::float8 AS pending_mqtt_edge_age_seconds;
