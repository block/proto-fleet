-- requires-shared: 1053

-- Maintenance adoption cannot attach a previous owner's private configuration
-- to newly allocated hardware, even if an import source was collected earlier.
CREATE FUNCTION validate_pool_assignment_adoption() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    configuration_owner BIGINT;
    equipment_owner BIGINT;
BEGIN
    IF NEW.operation_id IS NOT NULL THEN RETURN NEW; END IF;
    SELECT COALESCE(account_id,0) INTO configuration_owner FROM pool_configuration
        WHERE organization_id=NEW.organization_id AND configuration_id=NEW.configuration_id;
    IF NEW.target_kind='hashboard_installation' THEN
        SELECT COALESCE(a.subclient_id,a.client_id,0) INTO equipment_owner
        FROM hashboard_installation i
        LEFT JOIN hosted_account_asset a ON a.organization_id=NEW.organization_id AND a.kind=7 AND a.asset_id=i.hashboard_id
        WHERE i.id=NEW.target_id AND i.device_id=NEW.device_id AND i.removed_at IS NULL;
    ELSE
        SELECT COALESCE(a.subclient_id,a.client_id,0) INTO equipment_owner
        FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
        LEFT JOIN hosted_account_asset a ON a.organization_id=d.org_id AND a.kind=5 AND a.asset_id=d.id
        WHERE d.id=NEW.target_id AND d.org_id=NEW.organization_id AND dd.device_class=1 AND d.deleted_at IS NULL;
        IF equipment_owner IS NULL THEN
            -- Whole-container ownership is derived from every current board,
            -- using the same complete membership as the import collector.
            -- Missing membership must not count as operator-owned hardware.
            SELECT min(COALESCE(a.subclient_id,a.client_id,0)) INTO equipment_owner
            FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
            JOIN container_tank t ON t.device_id=d.id AND t.present
            JOIN container_module m ON m.tank_id=t.id AND m.retired_at IS NULL
            LEFT JOIN hosted_module_membership membership ON membership.module_id=m.id AND membership.org_id=d.org_id
            LEFT JOIN LATERAL unnest(membership.board_ids) b(id) ON true
            LEFT JOIN hosted_account_asset a ON a.organization_id=d.org_id AND a.kind=7 AND a.asset_id=b.id
            WHERE d.id=NEW.target_id AND d.org_id=NEW.organization_id AND dd.device_class=2
              AND d.deleted_at IS NULL AND dd.deleted_at IS NULL
            HAVING count(*)>0 AND bool_and(membership.module_id IS NOT NULL)
              AND count(DISTINCT COALESCE(a.subclient_id,a.client_id,0))=1;
        END IF;
    END IF;
    IF equipment_owner IS NULL OR configuration_owner IS DISTINCT FROM equipment_owner THEN
        RAISE EXCEPTION 'pool adoption requires the current equipment owner' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER pool_assignment_adoption BEFORE INSERT ON pool_configuration_assignment
    FOR EACH ROW EXECUTE FUNCTION validate_pool_assignment_adoption();

-- An immutable, rendered execution snapshot, not another user-facing library.
CREATE TABLE pool_configuration_group_snapshot (
 snapshot_id VARCHAR(36) PRIMARY KEY,
 organization_id BIGINT NOT NULL,
 device_id BIGINT NOT NULL,
 configuration_id VARCHAR(36) NOT NULL,
 revision_id VARCHAR(36) NOT NULL,
 slots JSONB NOT NULL CHECK (valid_pool_configuration_slots(slots)),
 CONSTRAINT fk_pool_configuration_group_snapshot_device FOREIGN KEY(device_id,organization_id) REFERENCES device(id,org_id),
 CONSTRAINT fk_pool_configuration_group_snapshot_revision FOREIGN KEY(organization_id,configuration_id,revision_id)
  REFERENCES pool_configuration_revision(organization_id,configuration_id,revision_id),
 CONSTRAINT uq_pool_configuration_group_snapshot_org_snapshot UNIQUE(organization_id,snapshot_id)
);
CREATE TRIGGER pool_configuration_group_snapshot_immutable BEFORE UPDATE OR DELETE ON pool_configuration_group_snapshot
 FOR EACH ROW EXECUTE FUNCTION keep_pool_configuration_revision_immutable();
ALTER TABLE pool_configuration_operation_target ADD COLUMN group_snapshot_id VARCHAR(36),
 ADD CONSTRAINT fk_pool_configuration_operation_target_group_snapshot FOREIGN KEY(organization_id,group_snapshot_id)
 REFERENCES pool_configuration_group_snapshot(organization_id,snapshot_id);
-- One controller workflow applies the entire selected set of board targets.
DROP INDEX idx_pool_configuration_operation_target_attempt;
CREATE UNIQUE INDEX idx_pool_configuration_operation_target_device_attempt
 ON pool_configuration_operation_target(physical_attempt_id) WHERE physical_attempt_id IS NOT NULL AND target_kind='device';
CREATE INDEX idx_pool_configuration_operation_target_group_attempt
 ON pool_configuration_operation_target(physical_attempt_id) WHERE target_kind='hashboard_installation';

-- Retain the admitted operation so pool updates can distinguish unrelated work.
-- Unclassified direct and workflow attempts remain conservative conflicts.
ALTER TABLE device_write_attempt ADD COLUMN command_type TEXT NOT NULL DEFAULT '';

-- Queue references capture the original command; do not infer legacy direct
-- operations from today's device state or an error message.
UPDATE device_write_attempt attempt SET command_type=message.command_type
FROM queue_message message WHERE message.id=attempt.queue_message_id;
