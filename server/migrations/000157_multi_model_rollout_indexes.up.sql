CREATE INDEX CONCURRENTLY idx_rollout_lane_model_binding_history_counts
    ON rollout_lane_model_binding(org_id, lane_id, lane_model_id, ended_at)
    INCLUDE (id);
