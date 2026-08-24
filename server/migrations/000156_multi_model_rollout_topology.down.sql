DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM rollout_lane_topology_cutover
        WHERE enabled
    ) OR EXISTS (
        SELECT 1 FROM firmware_rollout_group
    ) OR EXISTS (
        SELECT 1 FROM rollout_lane_topology_admin_operation
    ) OR EXISTS (
        SELECT 1
        FROM rollout_lane_model_channel
        WHERE origin <> 'legacy_backfill'
    ) OR EXISTS (
        SELECT 1
        FROM rollout_lane_model_binding
        WHERE origin <> 'legacy_backfill'
    ) OR EXISTS (
        SELECT 1
        FROM rollout_lane_model
        WHERE origin <> 'legacy_backfill'
    ) THEN
        RAISE EXCEPTION
            'cannot downgrade after rollout lane model topology history exists';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS rollout_lane_physical_membership_consistent
    ON device_set_membership;
DROP FUNCTION IF EXISTS validate_enabled_rollout_lane_physical_membership();

DROP TRIGGER IF EXISTS rollout_lane_model_binding_consistent
    ON rollout_lane_model_binding;
DROP FUNCTION IF EXISTS validate_rollout_lane_model_binding();

DROP VIEW IF EXISTS rollout_lane_topology_anomaly;
DROP FUNCTION IF EXISTS backfill_rollout_lane_model_topology(BIGINT);

ALTER TABLE rollout_lane_model_binding
    DROP CONSTRAINT IF EXISTS fk_rollout_lane_model_binding_end_operation;

DROP TRIGGER IF EXISTS rollout_lane_topology_admin_operation_truncate_immutable
    ON rollout_lane_topology_admin_operation;
DROP TRIGGER IF EXISTS rollout_lane_topology_admin_operation_immutable
    ON rollout_lane_topology_admin_operation;
DROP FUNCTION IF EXISTS reject_rollout_lane_topology_admin_operation_mutation();

DROP TABLE IF EXISTS rollout_lane_topology_admin_operation;

DROP TRIGGER IF EXISTS update_rollout_lane_topology_cutover_updated_at
    ON rollout_lane_topology_cutover;
DROP TABLE IF EXISTS rollout_lane_topology_cutover;

DROP TABLE IF EXISTS rollout_lane_active_parent;
DROP TABLE IF EXISTS firmware_rollout_group_model;

DROP TRIGGER IF EXISTS firmware_rollout_group_result_revision
    ON firmware_rollout_group;
DROP FUNCTION IF EXISTS maintain_firmware_rollout_group_result_revision();
DROP TRIGGER IF EXISTS update_firmware_rollout_group_updated_at
    ON firmware_rollout_group;
DROP TABLE IF EXISTS firmware_rollout_group;

DROP TABLE IF EXISTS rollout_lane_model_binding;
DROP TABLE IF EXISTS rollout_lane_model_channel;

DROP TRIGGER IF EXISTS update_rollout_lane_model_updated_at
    ON rollout_lane_model;
DROP TABLE IF EXISTS rollout_lane_model;

DROP FUNCTION IF EXISTS rollout_model_identity_v1(TEXT, TEXT);

ALTER TABLE discovered_device
    DROP COLUMN IF EXISTS model_identity_observed_at;
