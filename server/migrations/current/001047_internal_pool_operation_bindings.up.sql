-- requires-shared: 1045
-- The asynchronous executor must use the physical identity, ownership and worker
-- inputs that were reviewed, rather than reconstructing intent from new values.
-- Empty bindings belong to imported/internal records and cannot authorize I/O.
ALTER TABLE pool_configuration_operation_target ADD COLUMN reviewed_binding TEXT NOT NULL DEFAULT '';

CREATE FUNCTION keep_pool_operation_target_identity() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.organization_id,NEW.operation_id,NEW.target_kind,NEW.target_id,NEW.assignment_version,NEW.reviewed_binding)
       IS DISTINCT FROM
       (OLD.organization_id,OLD.operation_id,OLD.target_kind,OLD.target_id,OLD.assignment_version,OLD.reviewed_binding) THEN
        RAISE EXCEPTION 'pool operation target identity is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER pool_operation_target_identity BEFORE UPDATE ON pool_configuration_operation_target
    FOR EACH ROW EXECUTE FUNCTION keep_pool_operation_target_identity();

-- Bind each claimed whole-device update to its separately durable physical
-- admission. Recovery can then distinguish settled work from unresolved I/O.
ALTER TABLE pool_configuration_operation_target ADD COLUMN physical_attempt_id UUID
    CONSTRAINT fk_pool_configuration_operation_target_attempt REFERENCES device_write_attempt(attempt_id);
CREATE UNIQUE INDEX idx_pool_configuration_operation_target_attempt
    ON pool_configuration_operation_target(physical_attempt_id) WHERE physical_attempt_id IS NOT NULL;

-- Keep periodic recovery proportional to unfinished work, not retained history.
CREATE INDEX idx_pool_configuration_operation_target_recovery
    ON pool_configuration_operation_target(physical_attempt_id)
    WHERE state = 'applying' OR (state = 'failed' AND result_code = 'write_uncertain');
-- Reallocation and supersession find unfinished history for one assignment.
CREATE INDEX idx_pool_configuration_operation_target_open
    ON pool_configuration_operation_target(organization_id, target_kind, target_id)
    WHERE state IN ('pending','applying','failed','different','applied_unverified');

CREATE FUNCTION validate_pool_update_attempt() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.physical_attempt_id IS NOT NULL AND
        NEW.physical_attempt_id IS DISTINCT FROM OLD.physical_attempt_id THEN
        RAISE EXCEPTION 'pool update attempt is immutable' USING ERRCODE = '23514';
    END IF;
    IF NEW.physical_attempt_id IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.physical_attempt_id IS NULL) AND NOT EXISTS (
        SELECT 1 FROM device_write_attempt w
        JOIN pool_configuration_assignment a ON a.organization_id = NEW.organization_id
            AND a.target_kind = NEW.target_kind AND a.target_id = NEW.target_id
        WHERE w.attempt_id = NEW.physical_attempt_id AND w.organization_id = a.organization_id
            AND w.device_id = a.device_id AND w.operation_kind = 'mutation' AND w.status = 'running'
            AND a.operation_id = NEW.operation_id AND a.version = NEW.assignment_version AND a.retired_at IS NULL
            AND NEW.state = 'applying'
    ) THEN
        RAISE EXCEPTION 'pool update requires its current physical admission' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER pool_update_attempt BEFORE INSERT OR UPDATE ON pool_configuration_operation_target
    FOR EACH ROW EXECUTE FUNCTION validate_pool_update_attempt();
