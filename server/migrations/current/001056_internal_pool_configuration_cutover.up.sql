-- requires-shared: 1053

-- Effective board intent survives operation outcomes. Pending work remains in
-- operation targets; only established effects or explicit adoption enter here.
ALTER TABLE pool_configuration_group_snapshot ADD CONSTRAINT uq_pool_group_snapshot_device
 UNIQUE(organization_id,device_id,snapshot_id);
CREATE TABLE pool_configuration_board_intent (
 organization_id BIGINT NOT NULL,
 installation_id BIGINT NOT NULL REFERENCES hashboard_installation(id),
 device_id BIGINT NOT NULL,
 hashboard_id BIGINT NOT NULL REFERENCES hashboard(id),
 mac_address TEXT NOT NULL CHECK(mac_address<>''),
 group_snapshot_id VARCHAR(36) NOT NULL,
 operation_id VARCHAR(36),
 physical_attempt_id UUID REFERENCES device_write_attempt(attempt_id),
 adopted_at TIMESTAMPTZ,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(organization_id,installation_id),
 CONSTRAINT fk_pool_board_intent_snapshot FOREIGN KEY(organization_id,device_id,group_snapshot_id)
  REFERENCES pool_configuration_group_snapshot(organization_id,device_id,snapshot_id),
 CONSTRAINT fk_pool_board_intent_operation FOREIGN KEY(organization_id,operation_id)
  REFERENCES pool_configuration_operation(organization_id,operation_id),
 CONSTRAINT chk_pool_board_intent_origin CHECK(operation_id IS NOT NULL OR adopted_at IS NOT NULL)
);
CREATE INDEX idx_pool_board_intent_device ON pool_configuration_board_intent(organization_id,device_id);
-- Only terminal verified outcomes establish pre-upgrade effects. Failed and
-- unfinished operations require the maintenance inventory/recovery procedure.
INSERT INTO pool_configuration_board_intent(organization_id,installation_id,device_id,hashboard_id,mac_address,group_snapshot_id,operation_id,physical_attempt_id)
SELECT DISTINCT ON(a.organization_id,a.target_id)
 a.organization_id,a.target_id,a.device_id,i.hashboard_id,i.metadata->>'macAddress',t.group_snapshot_id,t.operation_id,t.physical_attempt_id
FROM pool_configuration_assignment a JOIN pool_configuration_operation_target t
 ON t.organization_id=a.organization_id AND t.target_kind=a.target_kind AND t.target_id=a.target_id
JOIN hashboard_installation i ON i.id=a.target_id
JOIN pool_configuration_group_snapshot snapshot ON snapshot.organization_id=t.organization_id AND snapshot.snapshot_id=t.group_snapshot_id
JOIN pool_configuration previous ON previous.organization_id=snapshot.organization_id AND previous.configuration_id=snapshot.configuration_id
JOIN pool_configuration current_config ON current_config.organization_id=a.organization_id AND current_config.configuration_id=a.configuration_id
WHERE a.target_kind='hashboard_installation' AND a.retired_at IS NULL
 AND t.state='verified' AND t.group_snapshot_id IS NOT NULL AND COALESCE(i.metadata->>'macAddress','')<>''
 AND previous.account_id IS NOT DISTINCT FROM current_config.account_id
 -- A retirement advances the assignment without creating an operation target.
 -- Do not resurrect effects across that gap, even if the same owner returns.
 AND (SELECT count(DISTINCT history.assignment_version) FROM pool_configuration_operation_target history
  WHERE history.organization_id=a.organization_id AND history.target_kind=a.target_kind AND history.target_id=a.target_id
   AND history.assignment_version>t.assignment_version AND history.assignment_version<=a.version)=a.version-t.assignment_version
ORDER BY a.organization_id,a.target_id,t.updated_at DESC,t.operation_id;


-- This marker is written only by the maintenance completeness gate. It is not
-- a rollout flag: returning to legacy writers requires restoring a backup.
CREATE TABLE pool_configuration_cutover (
 organization_id BIGINT PRIMARY KEY REFERENCES organization(id),
 frozen_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 completed_at TIMESTAMPTZ,
 report JSONB NOT NULL DEFAULT '{}'
);
CREATE FUNCTION reject_legacy_pool_writer() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE org BIGINT;
BEGIN
 org := COALESCE((to_jsonb(NEW)->>'organization_id')::bigint,(to_jsonb(NEW)->>'org_id')::bigint,
                 (to_jsonb(OLD)->>'organization_id')::bigint,(to_jsonb(OLD)->>'org_id')::bigint);
 -- A dedicated barrier avoids blocking ordinary organization foreign-key checks.
 -- Serialize with freeze, including writers that began before it.
 PERFORM pg_advisory_xact_lock_shared(hashtextextended('pool-cutover:'||org::text,0));
 IF EXISTS(SELECT 1 FROM pool_configuration_cutover WHERE organization_id=org) THEN
  RAISE EXCEPTION 'legacy pool writes are disabled after configuration cutover' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER legacy_pool_cutover BEFORE INSERT OR UPDATE OR DELETE ON pool
 FOR EACH ROW EXECUTE FUNCTION reject_legacy_pool_writer();
CREATE TRIGGER legacy_hosted_pool_cutover BEFORE INSERT OR UPDATE OR DELETE ON hosted_account_pool
 FOR EACH ROW EXECUTE FUNCTION reject_legacy_pool_writer();
CREATE TRIGGER legacy_pool_revision_cutover BEFORE INSERT OR UPDATE OR DELETE ON hosted_account_pool_revision
 FOR EACH ROW EXECUTE FUNCTION reject_legacy_pool_writer();
CREATE TRIGGER legacy_pool_profile_cutover BEFORE INSERT OR UPDATE OR DELETE ON hosted_account_pool_profile
 FOR EACH ROW EXECUTE FUNCTION reject_legacy_pool_writer();
CREATE TRIGGER legacy_board_intent_cutover BEFORE INSERT OR UPDATE OR DELETE ON hashboard_pool_intent
 FOR EACH ROW EXECUTE FUNCTION reject_legacy_pool_writer();

-- Legacy pool commands carry raw payloads and can bypass the old library.
-- Canonical pool applications use the fenced worker directly, never this queue.
CREATE FUNCTION reject_legacy_pool_command() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE org BIGINT;
BEGIN
 IF NEW.command_type<>'UpdateMiningPools' OR NEW.status NOT IN ('PENDING','PROCESSING') THEN RETURN NEW; END IF;
 SELECT org_id INTO org FROM device WHERE id=NEW.device_id;
 PERFORM pg_advisory_xact_lock_shared(hashtextextended('pool-cutover:'||org::text,0));
 IF EXISTS(SELECT 1 FROM pool_configuration_cutover WHERE organization_id=org) THEN
  RAISE EXCEPTION 'legacy pool commands are disabled; use reviewed pool configurations' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER legacy_pool_command_cutover BEFORE INSERT OR UPDATE OF command_type,status ON queue_message
 FOR EACH ROW EXECUTE FUNCTION reject_legacy_pool_command();
